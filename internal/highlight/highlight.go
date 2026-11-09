// Package highlight isolates Chroma from the UI with bounded input and output.
package highlight

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

const HelperFlag = "--syntax-helper"
const MaxBytes = 1 << 20
const MaxSpans = 100000
const Timeout = 2 * time.Second
const Plain = uint32(0x55ffff)

// Offsets are UTF-8 bytes, matching the editor; no token text is retained.
type Span struct {
	Start, End int
	Color      uint32
}
type Result struct {
	Language string
	Spans    []Span
}

func (r Result) Color(at int) uint32 {
	i := sort.Search(len(r.Spans), func(i int) bool { return r.Spans[i].End > at })
	if i < len(r.Spans) && r.Spans[i].Start <= at {
		return r.Spans[i].Color
	}
	return Plain
}

func tokenColor(t chroma.TokenType) uint32 {
	switch {
	case t.InCategory(chroma.Comment):
		return 0xaaaaaa
	case t.InCategory(chroma.Keyword):
		return 0xffffff
	case t.InSubCategory(chroma.LiteralString):
		return 0xffff55
	case t.InSubCategory(chroma.LiteralNumber):
		return 0x55ff55
	case t == chroma.NameFunction || t == chroma.NameClass || t == chroma.NameBuiltin:
		return 0x55ff55
	case t == chroma.NameTag || t == chroma.NameAttribute:
		return 0xff88ff
	case t.InCategory(chroma.Operator):
		return 0xffffff
	case t == chroma.Error:
		return 0xff8888
	}
	return Plain
}

// Parse runs only in a disposable helper in production: even an uncooperative
// lexer can be killed without leaving a goroutine or a regex cache in the GUI.
func Parse(path, text string) (result Result, err error) {
	defer func() {
		if p := recover(); p != nil {
			result = Result{}
			err = fmt.Errorf("lexer: %v", p)
		}
	}()
	if len(text) > MaxBytes {
		return Result{}, errors.New("syntax size limit")
	}
	lexer := lexers.Match(filepath.Base(path))
	if lexer == nil {
		lexer = lexers.Analyse(text[:min(len(text), 8192)])
	}
	if lexer == nil {
		return Result{}, nil
	}
	result.Language = lexer.Config().Name
	input := text
	if lexer.Config().EnsureNL && !strings.HasSuffix(input, "\n") {
		input += "\n"
	}
	it, err := lexer.Tokenise(&chroma.TokeniseOptions{State: "root", EnsureLF: false}, input)
	if err != nil {
		return Result{}, err
	}
	at, count := 0, 0
	for t := it(); t != chroma.EOF; t = it() {
		count++
		if count > MaxSpans*4 {
			return Result{}, errors.New("syntax token limit")
		}
		end := at + len(t.Value)
		// A lexer must preserve the text; do not paint incorrect offsets otherwise.
		if end > len(input) || input[at:end] != t.Value {
			return Result{}, errors.New("lexer changed input")
		}
		color := tokenColor(t.Type)
		visibleEnd := min(end, len(text))
		if color != Plain && visibleEnd > at {
			n := len(result.Spans)
			if n > 0 && result.Spans[n-1].End == at && result.Spans[n-1].Color == color {
				result.Spans[n-1].End = visibleEnd
			} else {
				if n >= MaxSpans {
					return Result{}, errors.New("syntax span limit")
				}
				result.Spans = append(result.Spans, Span{at, visibleEnd, color})
			}
		}
		at = end
	}
	if at != len(input) {
		return Result{}, errors.New("incomplete lexer output")
	}
	return result, nil
}

// HelperMain is dispatched before any Cocoa or application initialization.
func HelperMain(path string) int {
	debug.SetMemoryLimit(64 << 20) // Soft Go heap target; input and output have hard caps.
	data, err := io.ReadAll(io.LimitReader(os.Stdin, MaxBytes+1))
	if err != nil || len(data) > MaxBytes {
		return 1
	}
	result, err := Parse(path, string(data))
	if err != nil {
		return 1
	}
	if json.NewEncoder(os.Stdout).Encode(result) != nil {
		return 1
	}
	return 0
}

var slots = make(chan struct{}, 2) // Across all windows, not two helpers per window.
func Run(ctx context.Context, path, text string) (Result, error) {
	if len(text) > MaxBytes {
		return Result{}, errors.New("syntax size limit")
	}
	select {
	case slots <- struct{}{}:
		defer func() { <-slots }()
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	exe, err := os.Executable()
	if err != nil {
		return Result{}, err
	}
	cmd := exec.CommandContext(ctx, exe, HelperFlag, filepath.Base(path))
	cmd.Stdin = strings.NewReader(text)
	output := &limitedOutput{}
	cmd.Stdout = output
	if err = cmd.Run(); err != nil {
		return Result{}, err
	}
	var result Result
	if err = json.Unmarshal(output.data, &result); err != nil {
		return Result{}, err
	}
	if len(result.Spans) > MaxSpans {
		return Result{}, errors.New("syntax span limit")
	}
	last := 0
	for _, s := range result.Spans {
		if s.Start < last || s.End <= s.Start || s.End > len(text) {
			return Result{}, errors.New("invalid syntax offsets")
		}
		last = s.End
	}
	return result, nil
}

type limitedOutput struct{ data []byte }

func (b *limitedOutput) Write(p []byte) (int, error) {
	if len(b.data)+len(p) > 8<<20 {
		return 0, errors.New("syntax output limit")
	}
	b.data = append(b.data, p...)
	return len(p), nil
}
