package downloader

import (
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"

	"github.com/burritocatai/civcat/internal/hfapi"
)

const (
	// Number of concurrent download workers.
	parallelWorkers = 8
	// Minimum chunk size (10 MB). Files smaller than this use single-stream.
	minChunkSize = 10 * 1024 * 1024
	// Minimum file size to attempt parallel download (50 MB).
	minParallelSize = 50 * 1024 * 1024
)

// chunk represents a byte range to download.
type chunk struct {
	index int
	start int64
	end   int64
}

// parallelDownload downloads a file using multiple concurrent HTTP range requests
// and writes the result to destPath. It reports cumulative bytes downloaded via progressFn.
// Returns total bytes written or an error.
func parallelDownload(
	client *hfapi.Client,
	repoID, filename string,
	totalSize int64,
	destPath string,
	progressFn func(downloaded int64),
) (int64, error) {
	// Pre-allocate the output file to the full size.
	outFile, err := os.Create(destPath)
	if err != nil {
		return 0, fmt.Errorf("creating output file: %w", err)
	}
	defer outFile.Close()

	if err := outFile.Truncate(totalSize); err != nil {
		return 0, fmt.Errorf("pre-allocating file: %w", err)
	}

	// Split into chunks.
	chunkSize := totalSize / int64(parallelWorkers)
	if chunkSize < minChunkSize {
		chunkSize = minChunkSize
	}

	var chunks []chunk
	for i, offset := int(0), int64(0); offset < totalSize; i++ {
		end := offset + chunkSize - 1
		if end >= totalSize {
			end = totalSize - 1
		}
		chunks = append(chunks, chunk{index: i, start: offset, end: end})
		offset = end + 1
	}

	var totalDownloaded atomic.Int64
	var firstErr error
	var errOnce sync.Once

	// Worker pool.
	chunkCh := make(chan chunk, len(chunks))
	for _, c := range chunks {
		chunkCh <- c
	}
	close(chunkCh)

	var wg sync.WaitGroup
	workers := parallelWorkers
	if len(chunks) < workers {
		workers = len(chunks)
	}

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ch := range chunkCh {
				if err := downloadChunk(client, repoID, filename, outFile, ch, &totalDownloaded, progressFn); err != nil {
					errOnce.Do(func() {
						firstErr = fmt.Errorf("chunk %d (bytes %d-%d): %w", ch.index, ch.start, ch.end, err)
					})
					return
				}
			}
		}()
	}

	wg.Wait()

	if firstErr != nil {
		return 0, firstErr
	}

	return totalSize, nil
}

func downloadChunk(
	client *hfapi.Client,
	repoID, filename string,
	outFile *os.File,
	ch chunk,
	totalDownloaded *atomic.Int64,
	progressFn func(downloaded int64),
) error {
	resp, err := client.DownloadRange(repoID, filename, ch.start, ch.end)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	buf := make([]byte, 64*1024)
	offset := ch.start

	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := outFile.WriteAt(buf[:n], offset); writeErr != nil {
				return fmt.Errorf("writing at offset %d: %w", offset, writeErr)
			}
			offset += int64(n)
			newTotal := totalDownloaded.Add(int64(n))
			if progressFn != nil {
				progressFn(newTotal)
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return fmt.Errorf("reading: %w", readErr)
		}
	}

	return nil
}
