package management

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const requestLogCatalogTTL = 5 * time.Second

type requestErrorLog struct {
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Modified int64  `json:"modified"`
}

type requestLogCatalog struct {
	directory    string
	directoryMod int64
	directoryLen int64
	expiresAt    time.Time
	fileNames    []string
	errorNames   []string
	errorLogs    []requestErrorLog
	detailsReady bool
}

var requestLogCatalogState = struct {
	sync.Mutex
	byDirectory map[string]*requestLogCatalog
}{byDirectory: make(map[string]*requestLogCatalog)}

func loadRequestLogCatalog(directory string) (*requestLogCatalog, error) {
	directory, errAbs := filepath.Abs(directory)
	if errAbs != nil {
		return nil, errAbs
	}
	info, errStat := os.Stat(directory)
	if errStat != nil {
		return nil, errStat
	}
	now := time.Now()
	modTime := info.ModTime().UnixNano()

	requestLogCatalogState.Lock()
	cached := requestLogCatalogState.byDirectory[directory]
	if cached != nil && cached.directoryMod == modTime && cached.directoryLen == info.Size() && now.Before(cached.expiresAt) {
		requestLogCatalogState.Unlock()
		return cached, nil
	}
	requestLogCatalogState.Unlock()

	entries, errRead := os.ReadDir(directory)
	if errRead != nil {
		return nil, errRead
	}
	catalog := &requestLogCatalog{
		directory:    directory,
		directoryMod: modTime,
		directoryLen: info.Size(),
		expiresAt:    now.Add(requestLogCatalogTTL),
		fileNames:    make([]string, 0, len(entries)),
		errorNames:   make([]string, 0),
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		catalog.fileNames = append(catalog.fileNames, name)
		if strings.HasPrefix(name, "error-") && strings.HasSuffix(name, ".log") {
			catalog.errorNames = append(catalog.errorNames, name)
		}
	}

	requestLogCatalogState.Lock()
	requestLogCatalogState.byDirectory[directory] = catalog
	requestLogCatalogState.Unlock()
	return catalog, nil
}

func requestErrorLogDetails(catalog *requestLogCatalog) ([]requestErrorLog, error) {
	if catalog == nil {
		return []requestErrorLog{}, nil
	}
	requestLogCatalogState.Lock()
	if catalog.detailsReady {
		result := append([]requestErrorLog(nil), catalog.errorLogs...)
		requestLogCatalogState.Unlock()
		return result, nil
	}
	requestLogCatalogState.Unlock()

	details := make([]requestErrorLog, 0, len(catalog.errorNames))
	for _, name := range catalog.errorNames {
		info, errInfo := os.Stat(filepath.Join(catalog.directory, name))
		if errInfo != nil {
			if os.IsNotExist(errInfo) {
				continue
			}
			return nil, errInfo
		}
		details = append(details, requestErrorLog{
			Name:     name,
			Size:     info.Size(),
			Modified: info.ModTime().Unix(),
		})
	}
	sort.Slice(details, func(i, j int) bool {
		if details[i].Modified == details[j].Modified {
			return details[i].Name > details[j].Name
		}
		return details[i].Modified > details[j].Modified
	})

	requestLogCatalogState.Lock()
	if !catalog.detailsReady {
		catalog.errorLogs = append([]requestErrorLog(nil), details...)
		catalog.detailsReady = true
	}
	result := append([]requestErrorLog(nil), catalog.errorLogs...)
	requestLogCatalogState.Unlock()
	return result, nil
}

func requestLogNameByID(catalog *requestLogCatalog, requestID string) string {
	if catalog == nil || requestID == "" {
		return ""
	}
	suffix := "-" + requestID + ".log"
	for _, name := range catalog.fileNames {
		if strings.HasSuffix(name, suffix) {
			return name
		}
	}
	return ""
}
