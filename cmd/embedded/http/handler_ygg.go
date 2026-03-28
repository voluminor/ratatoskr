package main

import (
	"encoding/hex"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zstd"
	"lukechampine.com/blake3"
)

// // // // // // // // // //

const chunkSize = 250 * 1024

func init() {
	_ = mime.AddExtensionType(".html", "text/html; charset=utf-8")
	_ = mime.AddExtensionType(".css", "text/css; charset=utf-8")
	_ = mime.AddExtensionType(".js", "application/javascript; charset=utf-8")
	_ = mime.AddExtensionType(".json", "application/json")
	_ = mime.AddExtensionType(".svg", "image/svg+xml")
	_ = mime.AddExtensionType(".png", "image/png")
	_ = mime.AddExtensionType(".ico", "image/x-icon")
	_ = mime.AddExtensionType(".woff2", "font/woff2")
}

// //

type yggFileHandlerObj struct {
	wwwAbs string
	enc    *zstd.Encoder
}

func newYggFileHandler(wwwPath string) http.Handler {
	abs, _ := filepath.Abs(wwwPath)
	enc, _ := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	return &yggFileHandlerObj{wwwAbs: abs, enc: enc}
}

// //

func (h *yggFileHandlerObj) resolveFile(urlPath string) (*os.File, os.FileInfo, error) {
	clean := path.Clean("/" + strings.TrimPrefix(urlPath, "/"))
	filePath := filepath.Join(h.wwwAbs, filepath.FromSlash(clean))

	fileAbs, err := filepath.Abs(filePath)
	if err != nil || !strings.HasPrefix(fileAbs+string(filepath.Separator), h.wwwAbs+string(filepath.Separator)) {
		return nil, nil, os.ErrPermission
	}

	f, err := os.Open(fileAbs)
	if err != nil {
		return nil, nil, err
	}
	stat, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	if stat.IsDir() {
		_ = f.Close()
		idxPath := filepath.Join(fileAbs, "index.html")
		f, err = os.Open(idxPath)
		if err != nil {
			return nil, nil, os.ErrNotExist
		}
		stat, err = f.Stat()
		if err != nil {
			_ = f.Close()
			return nil, nil, err
		}
	}
	return f, stat, nil
}

func (h *yggFileHandlerObj) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f, stat, err := h.resolveFile(r.URL.Path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()

	hasher := blake3.New(32, nil)
	if _, err := io.Copy(hasher, f); err != nil {
		http.Error(w, "hash error", http.StatusInternalServerError)
		return
	}
	etag := `"` + hex.EncodeToString(hasher.Sum(nil)) + `"`

	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		http.Error(w, "seek error", http.StatusInternalServerError)
		return
	}

	ct := mime.TypeByExtension(filepath.Ext(stat.Name()))
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)

	acceptsZstd := strings.Contains(r.Header.Get("Accept-Encoding"), "zstd")
	if acceptsZstd {
		w.Header().Set("Content-Encoding", "zstd")
	}

	fileSize := stat.Size()
	flusher, _ := w.(http.Flusher)

	if fileSize > chunkSize {
		h.serveChunked(w, f, acceptsZstd, flusher)
	} else {
		h.serveSmall(w, f, acceptsZstd)
	}
}

func (h *yggFileHandlerObj) serveChunked(
	w http.ResponseWriter,
	f *os.File,
	compress bool,
	flusher http.Flusher,
) {
	buf := make([]byte, chunkSize)
	for {
		n, readErr := io.ReadFull(f, buf)
		if n > 0 {
			chunk := buf[:n]
			var payload []byte
			if compress {
				payload = h.enc.EncodeAll(chunk, nil)
			} else {
				payload = chunk
			}
			_, _ = w.Write(payload)
			if flusher != nil {
				flusher.Flush()
			}
		}
		if readErr != nil {
			break
		}
	}
}

func (h *yggFileHandlerObj) serveSmall(w http.ResponseWriter, f *os.File, compress bool) {
	data, err := io.ReadAll(f)
	if err != nil {
		return
	}
	if compress {
		_, _ = w.Write(h.enc.EncodeAll(data, nil))
	} else {
		_, _ = w.Write(data)
	}
}
