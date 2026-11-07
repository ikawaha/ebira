// Command winicon writes the icon of the app for Windows as a .syso, a
// resource the go command links into ebira-desktop.exe, so Explorer, the
// taskbar and the title bar show it. The picture is the favicon of the web
// UI, drawn at each size of a Windows icon (scaling one picture down blurs
// the small ones). Wails loads the icon of its windows from resource 3.
//
//	go run ./winicon -arch amd64 -o icon_windows_amd64.syso favicon.svg
package main

import (
	"flag"
	"fmt"
	"image"
	"os"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
	"github.com/tc-hib/winres"
)

// sizes are the sizes of the pictures in the icon, for Explorer and the taskbar
// at each scale of the display.
var sizes = []int{16, 20, 24, 32, 40, 48, 64, 128, 256}

var arches = map[string]winres.Arch{
	"amd64": winres.ArchAMD64,
	"arm64": winres.ArchARM64,
	"386":   winres.ArchI386,
}

func main() {
	arch := flag.String("arch", "amd64", "the GOARCH of the app")
	out := flag.String("o", "", "the .syso to write")
	flag.Parse()
	if *out == "" || flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: winicon -arch GOARCH -o FILE.syso ICON.svg")
		os.Exit(2)
	}
	if err := run(flag.Arg(0), *arch, *out); err != nil {
		fmt.Fprintln(os.Stderr, "winicon:", err)
		os.Exit(1)
	}
}

func run(svg, arch, out string) error {
	a, ok := arches[arch]
	if !ok {
		return fmt.Errorf("arch %q is not supported", arch)
	}
	f, err := os.Open(svg)
	if err != nil {
		return err
	}
	defer f.Close()
	icon, err := oksvg.ReadIconStream(f, oksvg.StrictErrorMode)
	if err != nil {
		return fmt.Errorf("%s: %w", svg, err)
	}
	images := make([]image.Image, len(sizes))
	for i, size := range sizes {
		images[i] = draw(icon, size)
	}
	ico, err := winres.NewIconFromImages(images)
	if err != nil {
		return err
	}
	var rs winres.ResourceSet
	if err := rs.SetIcon(winres.ID(3), ico); err != nil {
		return err
	}
	w, err := os.Create(out)
	if err != nil {
		return err
	}
	if err := rs.WriteObject(w, a); err != nil {
		w.Close()
		return err
	}
	return w.Close()
}

// draw draws the picture to fill a square of size pixels.
func draw(icon *oksvg.SvgIcon, size int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	icon.SetTarget(0, 0, float64(size), float64(size))
	scanner := rasterx.NewScannerGV(size, size, img, img.Bounds())
	icon.Draw(rasterx.NewDasher(size, size, scanner), 1)
	return img
}
