//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

// Foreign images (SP-0004 Q22, P4 section 5): .iso, .vhd and .vhdx mount
// through Windows' own disk-image support, the Storage cmdlets that the S0
// measurement already used. A small capability that shares no code with the
// container stack, and registers nothing: Windows owns those extensions.
//
// An .iso mounts without elevation on client Windows; a .vhd or .vhdx needs
// it, and that is said before the consent prompt, not found out through a
// failure. The elevated step for an image is _image; it mounts or dismounts
// the one image and does nothing else. FileDO keeps no state for an image:
// `filedo X: unmount` asks Windows which image X: belongs to.

func vdImageKind(p string) string {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".iso":
		return "iso"
	case ".vhd", ".vhdx":
		return "vhd"
	}
	return ""
}

// vdImageMount: filedo <image> mount [ro].
func vdImageMount(args []string, batch bool) error {
	path := args[0]
	ro := false
	for _, a := range args[1:] {
		switch strings.ToLower(a) {
		case "ro", "readonly":
			ro = true
		default:
			// Never quoted back: the word may be a password typed by mistake.
			return vdUsagef("unknown word after mount for an image (want ro; Windows chooses the drive letter)")
		}
	}
	abs, err := absPath(path)
	if err != nil {
		return err
	}
	if _, err := os.Stat(abs); err != nil {
		return vdUsagef("%s does not exist", abs)
	}
	var letters string
	if vdImageKind(abs) == "iso" {
		letters, err = vdImageRun(abs, false, true)
	} else {
		if !windows.GetCurrentProcessToken().IsElevated() && !batch {
			fmt.Println("Windows mounts a .vhd or .vhdx only with administrator rights; it will ask for consent now.")
		}
		var res vdResult
		res, err = vdRunElevated("_image", vdRequest{ImagePath: abs, ReadOnly: ro}, batch, nil)
		letters = res.Letter
	}
	if err != nil {
		return err
	}
	vdLogf("image mount %s: %s", abs, letters)
	if fields := strings.Fields(letters); len(fields) > 0 {
		vdUpdateState(func(s *vdState) error {
			s.Images = append(vdDropImage(s.Images, abs), vdImageRow{Letter: fields[0], Path: abs, MountedAt: time.Now()})
			return nil
		})
	}
	if letters == "" {
		fmt.Printf("Mounted %s. Windows gave its volumes no drive letter; see Disk Management.\n", abs)
		return nil
	}
	fmt.Printf("Mounted %s at %s. Unmount with: filedo %s unmount\n", abs, letters, strings.Fields(letters)[0])
	return nil
}

// vdImageOfLetter names the image FileDO mounted at letter, or "" when there
// is none. Windows answers "which letter has this image" but not the reverse,
// so the answer comes from the state file, and Windows is asked only whether
// that image is still attached and still at that letter.
func vdImageOfLetter(letter string) string {
	s, err := vdLoadState()
	if err != nil {
		return ""
	}
	l := strings.ToUpper(strings.TrimSuffix(letter, `\`))
	for _, im := range s.Images {
		if !strings.EqualFold(im.Letter, l) {
			continue
		}
		out, err := vdPowerShell("$imagePath = " + psDataExpr(im.Path) + "\n$i = Get-DiskImage -ImagePath $imagePath -ErrorAction SilentlyContinue\nif ($i.Attached) { @($i | Get-Volume -ErrorAction SilentlyContinue | ForEach-Object { \"$($_.DriveLetter):\" }) -join ' ' }\n")
		if err == nil && strings.Contains(strings.ToUpper(out), l) {
			return im.Path
		}
		// Ejected in Explorer, or the letter moved: the row is stale.
		vdUpdateState(func(s *vdState) error { s.Images = vdDropImage(s.Images, im.Path); return nil })
	}
	return ""
}

func vdDropImage(rows []vdImageRow, path string) []vdImageRow {
	kept := rows[:0]
	for _, r := range rows {
		if !strings.EqualFold(r.Path, path) {
			kept = append(kept, r)
		}
	}
	return kept
}

// vdImageUnmount dismounts the image at a drive letter, or the image file
// named; handled reports whether the target was an image at all.
func vdImageUnmount(target string, batch bool) (handled bool, err error) {
	img, letter := "", target
	if vdImageKind(target) != "" {
		if img, err = absPath(target); err != nil {
			return true, err
		}
		letter = "the image"
	} else {
		img = vdImageOfLetter(target)
	}
	if img == "" {
		return false, nil
	}
	if vdImageKind(img) == "iso" {
		_, err = vdImageRun(img, true, false)
	} else {
		if !windows.GetCurrentProcessToken().IsElevated() && !batch {
			fmt.Println("Windows dismounts a .vhd or .vhdx only with administrator rights; it will ask for consent now.")
		}
		_, err = vdRunElevated("_image", vdRequest{ImagePath: img, ImageDetach: true}, batch, nil)
	}
	if err != nil {
		return true, err
	}
	vdLogf("image unmount %s from %s", img, letter)
	vdUpdateState(func(s *vdState) error { s.Images = vdDropImage(s.Images, img); return nil })
	fmt.Printf("Unmounted %s (%s).\n", letter, img)
	return true, nil
}

// vdImageStep is the elevated half for a .vhd or .vhdx: it mounts or
// dismounts that one image and nothing else.
func vdImageStep(req vdRequest) (vdResult, error) {
	var res vdResult
	if vdImageKind(req.ImagePath) == "" {
		return res, vdUsagef("not a disk image: %s", req.ImagePath)
	}
	letters, err := vdImageRun(req.ImagePath, req.ImageDetach, req.ReadOnly)
	res.Letter = letters
	return res, err
}

// vdImageRun mounts (or dismounts) one image and returns the drive letters of
// its volumes.
func vdImageRun(path string, dismount, ro bool) (string, error) {
	script := vdImageScript(path, dismount, ro)
	out, err := vdPowerShell(script)
	if err != nil {
		if strings.Contains(out, "0x80070005") || strings.Contains(strings.ToLower(out), "access is denied") {
			return "", errTransport("Windows refused: this image needs administrator rights (" + strings.TrimSpace(out) + ")")
		}
		if strings.Contains(strings.ToLower(out), "in use") || strings.Contains(out, "0x80070020") {
			return "", errBusy(strings.TrimSpace(out))
		}
		return "", fmt.Errorf("Windows could not %s the image: %s", map[bool]string{true: "dismount", false: "mount"}[dismount], strings.TrimSpace(out))
	}
	letters := strings.TrimSpace(out)
	// A mount answers with drive letters, or with nothing when the image has no
	// lettered volume. Anything else is not an answer: it must never be printed
	// as the drive or recorded as a mounted disk (AUD-34-F2).
	if !dismount && !vdDriveLetters.MatchString(letters) {
		first := strings.TrimSpace(strings.SplitN(letters, "\n", 2)[0])
		return "", fmt.Errorf("Windows could not mount the image: %s", first)
	}
	return letters, nil
}

// The image path is data in every cmdlet call, including the second lookup
// after mount. Keep this script in one place so the test exercises its exact
// argument construction (AUD-34-F4).
func vdImageScript(path string, dismount, ro bool) string {
	pathAssignment := "$imagePath = " + psDataExpr(path) + "\n"
	var script string
	if dismount {
		script = pathAssignment + "Dismount-DiskImage -ImagePath $imagePath | Out-Null\n"
	} else {
		access := "ReadWrite"
		if ro {
			access = "ReadOnly"
		}
		script = pathAssignment + "$img = Mount-DiskImage -ImagePath $imagePath -Access " + access + " -PassThru\n" +
			"if (-not $img -or -not (Get-DiskImage -ImagePath $imagePath).Attached) { throw 'Windows did not attach the image' }\n" +
			"Start-Sleep -Milliseconds 500\n" +
			"$l = @($img | Get-Volume -ErrorAction SilentlyContinue | Where-Object DriveLetter | ForEach-Object { \"$($_.DriveLetter):\" })\n" +
			"if (-not $l) { $l = @(Get-DiskImage -ImagePath $imagePath | Get-Disk -ErrorAction SilentlyContinue | Get-Partition -ErrorAction SilentlyContinue | Where-Object DriveLetter | ForEach-Object { \"$($_.DriveLetter):\" }) }\n" +
			"$l -join ' '\n"
	}
	return script
}

// vdDriveLetters matches what a successful image mount prints: `E:`, `E: F:`
// or nothing at all.
var vdDriveLetters = regexp.MustCompile(`^([A-Za-z]:( [A-Za-z]:)*)?$`)
