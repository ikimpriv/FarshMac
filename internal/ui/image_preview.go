package ui

type ImagePreview struct {
	Data       string `json:"data,omitempty"`
	Full       bool   `json:"full,omitempty"` // The F3 image view: whole window, black backdrop.
	X, Y, W, H int
}

func (a *App) imagePreview() *ImagePreview {
	if v := a.Image; v != nil && !a.TermVisible {
		if v.Data == "" || a.Form != nil || a.Dialog != nil {
			a.previewSent = ""
			return nil
		}
		result := &ImagePreview{Full: true, X: 0, Y: 0, W: a.Cols, H: a.Rows}
		if key := "image:" + v.Path; a.previewSent != key {
			result.Data = v.Data
			a.previewSent = key
		}
		return result
	}
	if a.Doc != nil || a.TermVisible || a.Busy || a.Menu != nil || a.Form != nil || a.Dialog != nil || a.previewImage == "" || a.previewPending {
		a.previewSent = ""
		return nil
	}
	for i, p := range a.Panels {
		source := &a.Panels[1-i]
		if p.View != "Quick view" || p.Hidden || !source.can(capQuickView) || source.virtual(source.Current()) || source.Current() != a.previewPath {
			continue
		}
		x, w := a.panelRect(i)
		if w < 8 || a.Rows < 14 {
			continue
		}
		result := &ImagePreview{X: x + 2, Y: 7, W: w - 4, H: a.Rows - 13}
		if a.previewSent != a.previewImage {
			result.Data = a.previewImage
			a.previewSent = a.previewImage
		}
		return result
	}
	a.previewSent = ""
	return nil
}
