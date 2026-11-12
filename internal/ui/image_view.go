package ui

import (
	"fmt"
	"os"
	"path/filepath"

	"farshmac/internal/fsops"
	"farshmac/internal/preview"
)

// imageViewPixels bounds the picture decoded for the full-window view; the
// quick view uses the smaller default thumbnail.
const imageViewPixels = 2048

// imageView is the full-window graphical view opened by F3 on an image or
// PDF: black background, the picture centred, Esc closes. The decode runs in
// the sandboxed preview helper, so a damaged file cannot take the UI down.
type imageView struct {
	Path    string
	Data    string // data: URL once decoded.
	Caption string // Name, size and dimensions, or the failure.
	Failed  bool
	op      *fsops.Operation
}

func (a *App) openImage(path string) {
	a.closeImage()
	v := &imageView{Path: path, Caption: "Loading " + filepath.Base(path) + "…", op: fsops.NewOperation("Viewing image")}
	a.Image = v
	a.Menu = nil
	a.FastFind = nil
	a.dirty = true
	go func() {
		info, err := os.Stat(path)
		var r preview.Result
		if err == nil {
			r, err = preview.RunSized(v.op.Context(), path, imageViewPixels)
		}
		a.jobs <- func() {
			if a.Image != v {
				return
			}
			a.dirty = true
			if err != nil {
				v.Failed = true
				v.Caption = filepath.Base(path) + ": cannot be shown (unsupported, damaged or too large)"
				if !os.IsNotExist(err) && !isCancelled(err) {
					a.Status = v.Caption
				} else if os.IsNotExist(err) {
					v.Caption = filepath.Base(path) + ": " + errorReason(err)
				}
				return
			}
			v.Data = r.Data
			v.Caption = fmt.Sprintf("%s · %d bytes", filepath.Base(path), info.Size())
			if r.Pages > 0 {
				v.Caption += fmt.Sprintf(" · PDF, page 1 of %d", r.Pages)
			} else {
				v.Caption += fmt.Sprintf(" · %d × %d", r.Width, r.Height)
			}
			a.recordView(path, false)
		}
	}()
}

func (a *App) closeImage() {
	if a.Image == nil {
		return
	}
	if a.Image.op != nil {
		a.Image.op.Cancel()
	}
	a.Image = nil
	a.previewSent = ""
	a.dirty = true
}

// imageKey: Esc (also F3, F10 and Enter) closes the view; other keys are ignored.
func (a *App) imageKey(m Message) {
	switch m.Key {
	case "Escape", "F3", "F10", "Enter":
		if !m.Shift && !m.Meta && !m.Ctrl && !m.Alt {
			a.closeImage()
		}
	}
}

// drawImage paints the black backdrop of the image view. The picture itself
// is an overlay the renderer positions over the whole grid (see imagePreview);
// only a caption is drawn while it loads or when it cannot be decoded.
func (a *App) drawImage(g *Grid) {
	g.Fill(0, 0, g.W, g.H, Gray, Black)
	v := a.Image
	if v.Data != "" {
		return
	}
	text := v.Caption
	if len([]rune(text)) > g.W-2 {
		text = fitStatus(text, g.W-2)
	}
	x := max(0, (g.W-len([]rune(text)))/2)
	g.Text(x, g.H/2, text, Gray, Black, g.W-x)
}
