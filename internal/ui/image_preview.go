package ui

type ImagePreview struct {
	Data       string `json:"data,omitempty"`
	X, Y, W, H int
}

func (a *App) imagePreview() *ImagePreview {
	if a.Doc != nil || a.TermVisible || a.Busy || a.Menu != nil || a.Form != nil || a.Dialog != nil || a.previewImage == "" || a.previewPending {
		a.previewSent = ""
		return nil
	}
	for i, p := range a.Panels {
		source := &a.Panels[1-i]
		if p.View != "Quick view" || p.Hidden || source.Remote != nil || source.Archive != nil || source.Current() != a.previewPath {
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
