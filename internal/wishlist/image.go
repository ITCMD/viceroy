package wishlist

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	_ "image/gif" // register decoders
	"image/jpeg"
	_ "image/png"
	"net/http"
)

// MaxImageBytes caps a stored item image (the app shrinks uploads before sending).
const MaxImageBytes = 256 << 10

// ImageSize is the longest side of a stored item image.
const ImageSize = 400

var ErrBadImage = errors.New("use a PNG, JPEG, WebP or GIF image")

// AllowedImage reports whether the sniffed type may be stored and served (no SVG, which could
// carry script).
func AllowedImage(mime string) bool {
	switch mime {
	case "image/png", "image/jpeg", "image/webp", "image/gif":
		return true
	}
	return false
}

// Shrink scales a downloaded image to at most ImageSize px on its longest side as a JPEG on
// white. WebP can't be decoded here, so a small one is kept as it is.
func Shrink(data []byte) ([]byte, string, error) {
	mime := http.DetectContentType(data)
	if !AllowedImage(mime) {
		return nil, "", ErrBadImage
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		if mime == "image/webp" && len(data) <= MaxImageBytes {
			return data, mime, nil
		}
		return nil, "", ErrBadImage
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return nil, "", ErrBadImage
	}
	if mime != "image/gif" && max(w, h) <= ImageSize && len(data) <= MaxImageBytes {
		return data, mime, nil
	}
	nw, nh := w, h
	if w >= h && w > ImageSize {
		nw, nh = ImageSize, max(1, h*ImageSize/w)
	} else if h > w && h > ImageSize {
		nw, nh = max(1, w*ImageSize/h), ImageSize
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	// Box filter: average the source pixels that fall in each destination pixel.
	for y := range nh {
		y0, y1 := b.Min.Y+y*h/nh, b.Min.Y+max((y+1)*h/nh, y*h/nh+1)
		for x := range nw {
			x0, x1 := b.Min.X+x*w/nw, b.Min.X+max((x+1)*w/nw, x*w/nw+1)
			var r, g, bl, n uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					cr, cg, cb, ca := src.At(sx, sy).RGBA()
					// Composite on white (colors are alpha-premultiplied).
					white := 0xffff - uint64(ca)
					r += uint64(cr) + white
					g += uint64(cg) + white
					bl += uint64(cb) + white
					n++
				}
			}
			dst.Set(x, y, color.RGBA64{uint16(r / n), uint16(g / n), uint16(bl / n), 0xffff})
		}
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 85}); err != nil {
		return nil, "", err
	}
	if out.Len() > MaxImageBytes {
		return nil, "", ErrBadImage
	}
	return out.Bytes(), "image/jpeg", nil
}
