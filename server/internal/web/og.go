package web

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// The note title renders in the same serif the default theme uses for titles.
// Crimson Pro is licensed under the SIL Open Font License (see fonts/OFL.txt).
//
//go:embed fonts/CrimsonPro-Italic-SemiBold.ttf
var crimsonItalicTTF []byte

const (
	ogWidth   = 1200
	ogHeight  = 630
	ogMargin  = 80
	ogTextW   = ogWidth - 2*ogMargin
	ogMaxRows = 4
	// Baseline of the title's last line — the block grows upward from here,
	// sitting just above the accent bar.
	ogLastBaseline = 556
)

var (
	ogBG       = color.RGBA{0x0f, 0x0f, 0x14, 0xff}
	ogTitleCol = color.RGBA{0xf4, 0xf4, 0xf5, 0xff}
	ogSiteCol  = color.RGBA{0xd6, 0xd6, 0xda, 0xff}
	// Crimson Pro has a small x-height, so title sizes run larger than the
	// sans equivalents would.
	ogTitleSizes = []int{76, 66, 56}
)

// ogRenderer draws 1200×630 share images: site name up top, the note title in
// italic serif anchored to the bottom, an accent bar along the bottom edge.
// Rendered images are cached in memory keyed by title.
type ogRenderer struct {
	siteName string
	accent   color.RGBA
	siteFace font.Face
	faces    map[int]font.Face // title faces by point size

	mu    sync.Mutex
	cache map[string][]byte
}

func newOGRenderer(siteName, accentHex string) (*ogRenderer, error) {
	title, err := opentype.Parse(crimsonItalicTTF)
	if err != nil {
		return nil, err
	}
	regular, err := opentype.Parse(goregular.TTF)
	if err != nil {
		return nil, err
	}
	r := &ogRenderer{
		siteName: siteName,
		accent:   parseHexColor(accentHex),
		faces:    make(map[int]font.Face),
		cache:    make(map[string][]byte),
	}
	if r.siteFace, err = opentype.NewFace(regular, &opentype.FaceOptions{Size: 37, DPI: 72, Hinting: font.HintingFull}); err != nil {
		return nil, err
	}
	for _, size := range ogTitleSizes {
		face, err := opentype.NewFace(title, &opentype.FaceOptions{Size: float64(size), DPI: 72, Hinting: font.HintingFull})
		if err != nil {
			return nil, err
		}
		r.faces[size] = face
	}
	return r, nil
}

// render returns the PNG bytes and an ETag for a title.
func (r *ogRenderer) render(title string) ([]byte, string, error) {
	sum := sha256.Sum256([]byte(r.siteName + "\x00" + title))
	key := hex.EncodeToString(sum[:16])
	etag := `"og-` + key + `"`

	r.mu.Lock()
	if b, ok := r.cache[key]; ok {
		r.mu.Unlock()
		return b, etag, nil
	}
	r.mu.Unlock()

	b, err := r.draw(title)
	if err != nil {
		return nil, "", err
	}

	r.mu.Lock()
	if len(r.cache) >= 256 {
		for k := range r.cache { // drop an arbitrary entry; personal scale
			delete(r.cache, k)
			break
		}
	}
	r.cache[key] = b
	r.mu.Unlock()
	return b, etag, nil
}

func (r *ogRenderer) draw(title string) ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, ogWidth, ogHeight))
	draw.Draw(img, img.Bounds(), image.NewUniform(ogBG), image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(0, ogHeight-10, ogWidth, ogHeight), image.NewUniform(r.accent), image.Point{}, draw.Src)

	drawString(img, r.siteFace, ogSiteCol, ogMargin, 124, r.siteName)

	// Pick the largest title size that fits in ogMaxRows lines.
	size := ogTitleSizes[len(ogTitleSizes)-1]
	var lines []string
	for _, cand := range ogTitleSizes {
		lines = wrapText(r.faces[cand], title, ogTextW)
		if len(lines) <= ogMaxRows {
			size = cand
			break
		}
	}
	face := r.faces[size]
	lines = wrapText(face, title, ogTextW)
	if len(lines) > ogMaxRows {
		lines = lines[:ogMaxRows]
		lines[ogMaxRows-1] += "…"
	}

	// Bottom-anchored: the last line's baseline is fixed and the block grows up.
	lineH := size * 115 / 100
	y := ogLastBaseline - (len(lines)-1)*lineH
	for _, line := range lines {
		drawString(img, face, ogTitleCol, ogMargin, y, ellipsize(face, line, ogTextW))
		y += lineH
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func drawString(dst draw.Image, face font.Face, col color.Color, x, y int, s string) {
	d := &font.Drawer{
		Dst:  dst,
		Src:  image.NewUniform(col),
		Face: face,
		Dot:  fixed.P(x, y),
	}
	d.DrawString(s)
}

func wrapText(face font.Face, text string, maxW int) []string {
	words := strings.Fields(text)
	var lines []string
	cur := ""
	for _, w := range words {
		cand := w
		if cur != "" {
			cand = cur + " " + w
		}
		if cur == "" || font.MeasureString(face, cand).Ceil() <= maxW {
			cur = cand
		} else {
			lines = append(lines, cur)
			cur = w
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	if len(lines) == 0 {
		lines = []string{"Untitled"}
	}
	return lines
}

func ellipsize(face font.Face, s string, maxW int) string {
	if font.MeasureString(face, s).Ceil() <= maxW {
		return s
	}
	runes := []rune(strings.TrimSuffix(s, "…"))
	for len(runes) > 0 {
		runes = runes[:len(runes)-1]
		cand := strings.TrimRight(string(runes), " ") + "…"
		if font.MeasureString(face, cand).Ceil() <= maxW {
			return cand
		}
	}
	return "…"
}

func parseHexColor(s string) color.RGBA {
	s = strings.TrimPrefix(s, "#")
	c := color.RGBA{0x6c, 0x5c, 0xe7, 0xff}
	if len(s) != 6 {
		return c
	}
	if v, err := strconv.ParseUint(s, 16, 32); err == nil {
		c = color.RGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 0xff}
	}
	return c
}
