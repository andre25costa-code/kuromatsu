package uninstall

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// zipDir writes every entry under src to a new zip file at dst (mode 0600).
// Symlinks are not followed: each one is stored as a link entry whose
// content is its target, and its path is returned. On error the partial
// zip is removed.
func zipDir(src, dst string) (symlinks []string, err error) {
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = f.Close()
			_ = os.Remove(dst)
		}
	}()

	zw := zip.NewWriter(f)
	err = filepath.WalkDir(src, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if p == src || p == dst {
			return nil
		}
		rel, relErr := filepath.Rel(src, p)
		if relErr != nil {
			return relErr
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		hdr, hdrErr := zip.FileInfoHeader(info)
		if hdrErr != nil {
			return hdrErr
		}
		hdr.Name = filepath.ToSlash(rel)

		switch mode := info.Mode(); {
		case mode.IsDir():
			hdr.Name += "/"
			_, createErr := zw.CreateHeader(hdr)
			return createErr
		case mode&fs.ModeSymlink != 0:
			target, linkErr := os.Readlink(p)
			if linkErr != nil {
				return linkErr
			}
			hdr.Method = zip.Store
			w, createErr := zw.CreateHeader(hdr)
			if createErr != nil {
				return createErr
			}
			if _, writeErr := io.WriteString(w, target); writeErr != nil {
				return writeErr
			}
			symlinks = append(symlinks, p)
			return nil
		case mode.IsRegular():
			hdr.Method = zip.Deflate
			w, createErr := zw.CreateHeader(hdr)
			if createErr != nil {
				return createErr
			}
			return copyFile(w, p)
		default:
			return fmt.Errorf("cannot back up %s: unsupported file type %s", p, mode.Type())
		}
	})
	if err != nil {
		return nil, err
	}
	if err = zw.Close(); err != nil {
		return nil, err
	}
	if err = f.Sync(); err != nil {
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	return symlinks, nil
}

func copyFile(w io.Writer, p string) error {
	r, err := os.Open(p)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, r)
	return errors.Join(err, r.Close())
}
