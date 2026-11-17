package ui

import (
	"fmt"
	"os"
	"path/filepath"

	"farshmac/internal/cloudfiles"
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
	Path                               string
	Data                               string // data: URL once decoded.
	Caption                            string // Name, size and dimensions, or the failure.
	Failed                             bool
	Page, Pages, PageWidth, PageHeight int
	loading                            bool
	op                                 *fsops.Operation
}

func (a *App) openImage(path string) {
	a.closeImage()
	a.loadImagePage(path, 1)
}

func (a *App) loadImagePage(path string, page int) {
	if a.Image != nil && a.Image.op != nil {
		a.Image.op.Cancel()
	}
	v := &imageView{Path: path, Page: page, loading: true, Caption: "Loading " + filepath.Base(path) + "…", op: fsops.NewOperation("Viewing image")}
	if previous := a.Image; previous != nil && previous.Path == path {
		v.Data, v.Pages, v.PageWidth, v.PageHeight = previous.Data, previous.Pages, previous.PageWidth, previous.PageHeight
	}
	a.Image = v
	a.Menu = nil
	a.FastFind = nil
	a.dirty = true
	go func() {
		resolved, err := cloudfiles.Prepare(v.op.Context(), path)
		var info os.FileInfo
		if err == nil {
			info, err = os.Stat(resolved)
		}
		var r preview.Result
		if err == nil {
			r, err = preview.RunPage(v.op.Context(), resolved, imageViewPixels, page)
		}
		a.jobs <- func() {
			if a.Image != v {
				return
			}
			v.loading = false
			a.previewSent = ""
			a.dirty = true
			if err != nil {
				v.Failed = true
				v.Caption = filepath.Base(path) + ": " + err.Error()
				if !os.IsNotExist(err) && !isCancelled(err) {
					a.Status = v.Caption
				} else if os.IsNotExist(err) {
					v.Caption = filepath.Base(path) + ": " + errorReason(err)
				}
				return
			}
			v.Data = r.Data
			v.Pages, v.PageWidth, v.PageHeight = r.Pages, r.Width, r.Height
			v.Caption = fmt.Sprintf("%s · %d bytes", filepath.Base(path), info.Size())
			if r.Pages > 0 {
				v.Caption += fmt.Sprintf(" · PDF, page %d of %d", page, r.Pages)
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

// imageKey handles PDF page navigation and closing the graphical viewer.
func (a *App) imageKey(m Message) {
	if v := a.Image; v != nil && v.Pages > 0 && !m.Meta && !m.Ctrl && !m.Alt {
		page := v.Page
		switch m.Key {
		case "PageDown", "]":
			page++
		case "PageUp", "[":
			page--
		case "Home":
			page = 1
		case "End":
			page = v.Pages
		}
		page = max(1, min(v.Pages, page))
		if page != v.Page {
			a.loadImagePage(v.Path, page)
			return
		}
	}
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
