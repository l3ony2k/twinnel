package main

import (
	"encoding/binary"
	"errors"
)

// icoPNG returns the PNG image of an .ico file's entry px pixels wide: the
// one of that size, else the smallest larger one, else the largest. Windows
// tray marks (assets/icons/tray/windows) are .ico files of PNG entries.
func icoPNG(ico []byte, px int) ([]byte, error) {
	if len(ico) < 6 || binary.LittleEndian.Uint16(ico[2:]) != 1 {
		return nil, errors.New("not an icon file")
	}
	n := int(binary.LittleEndian.Uint16(ico[4:]))
	// pick: the exact size, else the smallest larger, else the largest.
	best, bestW := -1, 0
	better := func(w int) bool {
		switch {
		case best < 0 || w == px:
			return true
		case bestW == px:
			return false
		case w > px:
			return bestW < px || w < bestW
		default:
			return bestW < px && w > bestW
		}
	}
	for i := 0; i < n; i++ {
		e := 6 + 16*i
		if e+16 > len(ico) {
			return nil, errors.New("icon directory cut short")
		}
		w := int(ico[e])
		if w == 0 {
			w = 256
		}
		if better(w) {
			best, bestW = i, w
		}
	}
	if best < 0 {
		return nil, errors.New("icon file has no images")
	}
	e := 6 + 16*best
	size := int(binary.LittleEndian.Uint32(ico[e+8:]))
	off := int(binary.LittleEndian.Uint32(ico[e+12:]))
	if off < 0 || size <= 0 || off+size > len(ico) {
		return nil, errors.New("icon entry out of range")
	}
	img := ico[off : off+size]
	if len(img) < 8 || string(img[1:4]) != "PNG" {
		return nil, errors.New("icon entry is not a PNG")
	}
	return img, nil
}

// trayIconPx is the notification area's icon size at a DPI: the small
// icon metric, 16 px at 96 DPI, scaled.
func trayIconPx(dpi int) int {
	if dpi <= 0 {
		dpi = 96
	}
	return (16*dpi + 48) / 96
}
