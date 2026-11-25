package artwork

import (
	"image"
	"image/draw"

	xdraw "golang.org/x/image/draw"
)

func fit(img image.Image, side int) *image.RGBA {
	b := img.Bounds()
	crop := min(b.Dx(), b.Dy())
	x, y := b.Min.X+(b.Dx()-crop)/2, b.Min.Y+(b.Dy()-crop)/2
	src := image.Rect(x, y, x+crop, y+crop)
	out := crop
	if side > 0 && side < crop {
		out = side
	}
	dst := image.NewRGBA(image.Rect(0, 0, out, out))
	if out == crop {
		draw.Draw(dst, dst.Bounds(), img, src.Min, draw.Src)
	} else {
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, src, xdraw.Src, nil)
	}
	return dst
}

func fitWide(img image.Image, side int) *image.RGBA {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return image.NewRGBA(image.Rect(0, 0, 1, 1))
	}
	outW, outH := w, h
	if side > 0 && max(w, h) > side {
		if w >= h {
			outW = side
			outH = max(1, h*side/w)
		} else {
			outH = side
			outW = max(1, w*side/h)
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, outW, outH))
	if outW == w && outH == h {
		draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Src)
	} else {
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, xdraw.Src, nil)
	}
	return dst
}
