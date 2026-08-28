package godwarf

import (
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"godwarf/clsnd"
	"godwarf/climg"
)

const (
	clImagesFile = "CL_Images"
	clSoundsFile = "CL_Sounds"
	currentCLVer = 1497
	updateBase   = "https://www.deltatao.com/downloads/clanlord"
	mirrorBase   = "https://m45sci.xyz/downloads/clanlord"
	dlTimeout    = 300 * time.Second
)

type dlProgress struct {
	mu       sync.Mutex
	active   bool
	name     string
	err      string
	done     bool
	bytesIn  int64
	bytesOut int64
}

var dlStateImages = &dlProgress{}
var dlStateSounds = &dlProgress{}

func (d *dlProgress) start(name string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.active = true
	d.name = name
	d.err = ""
	d.done = false
	d.bytesIn = 0
	d.bytesOut = 0
}

func (d *dlProgress) setProgress(bytesIn, bytesOut int64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.bytesIn = bytesIn
	d.bytesOut = bytesOut
}

func (d *dlProgress) finish(err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.active = false
	d.done = true
	if err != nil {
		d.err = err.Error()
	}
}

func (d *dlProgress) reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.active = false
	d.done = false
	d.err = ""
	d.bytesIn = 0
	d.bytesOut = 0
}

func (d *dlProgress) snapshot() (active bool, name string, err string, done bool, bytesIn, bytesOut int64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.active, d.name, d.err, d.done, d.bytesIn, d.bytesOut
}

// readKeyFileVersion reads the version number from a keyfile (CL_Images or CL_Sounds).
func readKeyFileVersion(path string) (uint32, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	var header [12]byte
	if _, err := io.ReadFull(f, header[:]); err != nil {
		return 0, err
	}
	count := int(binary.BigEndian.Uint32(header[2:6]))

	entry := make([]byte, 16)
	for i := 0; i < count; i++ {
		if _, err := io.ReadFull(f, entry); err != nil {
			return 0, err
		}
		typ := binary.BigEndian.Uint32(entry[8:12])
		id := binary.BigEndian.Uint32(entry[12:16])
		pos := binary.BigEndian.Uint32(entry[0:4])
		size := binary.BigEndian.Uint32(entry[4:8])
		if typ == 0x56657273 && id == 0 { // kTypeVersion = 'Vers'
			if _, err := f.Seek(int64(pos), io.SeekStart); err != nil {
				return 0, err
			}
			buf := make([]byte, size)
			if _, err := io.ReadFull(f, buf); err != nil {
				return 0, err
			}
			v := binary.BigEndian.Uint32(buf)
			if v <= 0xFF {
				v <<= 8
			}
			return v, nil
		}
	}
	return 0, errors.New("version record not found")
}

// downloadCL downloads a gzipped keyfile from the server, reporting progress
// to the given dlProgress (images vs sounds have separate states since the two
// downloads run concurrently).
func downloadCL(url, dest string, state *dlProgress) error {
	client := &http.Client{Timeout: dlTimeout}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}

	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return fmt.Errorf("gzip reader: %w", err)
	}
	defer gz.Close()

	tmp := dest + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create %s: %w", tmp, err)
	}

	var totalIn int64
	if resp.ContentLength > 0 {
		totalIn = resp.ContentLength
	}

	// Wrap gz in a progress-reporting reader
	pr := &progressReader{r: gz, state: state, totalIn: totalIn}
	if _, err := io.Copy(f, pr); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("copy: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("close: %w", err)
	}
	return os.Rename(tmp, dest)
}

// progressReader wraps an io.Reader and reports bytes read to a dlProgress.
type progressReader struct {
	r       io.Reader
	state   *dlProgress
	totalIn int64
	read    int64
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.r.Read(p)
	pr.read += int64(n)
	// BytesOut = decompressed bytes written to file (approximate)
	// BytesIn = compressed bytes downloaded
	pr.state.setProgress(pr.read, pr.read)
	return n, err
}

// downloadKeyfile downloads a gzipped keyfile, trying the official server
// first and the mirror (m45sci.xyz) if the official download fails.
func downloadKeyfile(file string, ver int, dest string, state *dlProgress) error {
	primary := fmt.Sprintf("%s/data/%s.%d.gz", updateBase, file, ver)
	if err := downloadCL(primary, dest, state); err == nil {
		flog(fmt.Sprintf("%s downloaded from official server", file))
		return nil
	} else {
		flog(fmt.Sprintf("%s: official download failed (%v); trying mirror", file, err))
	}
	backup := fmt.Sprintf("%s/data/%s.%d.gz", mirrorBase, file, ver)
	if err := downloadCL(backup, dest, state); err != nil {
		return fmt.Errorf("download %s from official server and mirror failed: %w", file, err)
	}
	flog(fmt.Sprintf("%s downloaded from mirror", file))
	return nil
}

// checkAndUpdateImages checks if CL_Images is present and current. If not, it
// downloads the latest version. Returns the loaded CLImages and the version.
func checkAndUpdateImages(dataDir string) (*climg.CLImages, int, error) {
	os.MkdirAll(dataDir, 0755)
	imgPath := filepath.Join(dataDir, clImagesFile)

	localVer := 0
	if v, err := readKeyFileVersion(imgPath); err == nil {
		localVer = int(v >> 8)
	}

	if localVer < currentCLVer {
		dlStateImages.start(fmt.Sprintf("CL_Images_%d", currentCLVer))
		flog(fmt.Sprintf("CL_Images v%d -> v%d, downloading...", localVer, currentCLVer))
		err := downloadKeyfile(clImagesFile, currentCLVer, imgPath, dlStateImages)
		dlStateImages.finish(err)
		if err != nil {
			return nil, 0, fmt.Errorf("download CL_Images: %w", err)
		}
		flog("CL_Images download complete")
	} else {
		flog(fmt.Sprintf("CL_Images v%d is current", localVer))
	}

	cl, err := climg.Load(imgPath)
	if err != nil {
		return nil, 0, fmt.Errorf("load CL_Images: %w", err)
	}
	return cl, currentCLVer, nil
}

// checkAndUpdateSounds checks if CL_Sounds is present and current. If not, it
// downloads the latest version. Returns the loaded CLSounds.
func checkAndUpdateSounds(dataDir string) (*clsnd.CLSounds, error) {
	os.MkdirAll(dataDir, 0755)
	sndPath := filepath.Join(dataDir, clSoundsFile)

	localVer := 0
	if v, err := readKeyFileVersion(sndPath); err == nil {
		localVer = int(v >> 8)
	}

	if localVer < currentCLVer {
		dlStateSounds.start(fmt.Sprintf("CL_Sounds_%d", currentCLVer))
		flog(fmt.Sprintf("CL_Sounds v%d -> v%d, downloading...", localVer, currentCLVer))
		err := downloadKeyfile(clSoundsFile, currentCLVer, sndPath, dlStateSounds)
		dlStateSounds.finish(err)
		if err != nil {
			return nil, fmt.Errorf("download CL_Sounds: %w", err)
		}
		flog("CL_Sounds download complete")
	} else {
		flog(fmt.Sprintf("CL_Sounds v%d is current", localVer))
	}

	snd, err := clsnd.Load(sndPath)
	if err != nil {
		return nil, fmt.Errorf("load CL_Sounds: %w", err)
	}
	return snd, nil
}
