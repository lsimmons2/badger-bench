package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"runtime/pprof"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dgraph-io/badger/y"
)

var (
	dir           = flag.String("dir", "datafiles", "File to read from")
	numReads      = flag.Int64("num", 2000000, "Number of reads")
	mode          = flag.Int("mode", 1, "0 = serial, 1 = parallel, 2 = parallel via channel")
	numGoroutines = flag.Int("jobs", 8, "Number of Goroutines")
	cpuprofile    = flag.String("cpuprofile", "", "write cpu profile to file")
)

var readSize int64 = 4 << 10

func getIndices(r *rand.Rand, flist []*os.File, maxFileSize int64) (*os.File, int64) {
	fidx := r.Intn(len(flist))
	iidx := r.Int63n(maxFileSize - readSize)
	return flist[fidx], iidx
}

func Serial(fList []*os.File, maxFileSize int64) {
	startT := time.Now()
	var i int64 = 0
	b := make([]byte, int(readSize))

	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	for ; i < *numReads; i++ {
		fd, offset := getIndices(r, fList, maxFileSize)
		_, err := fd.ReadAt(b, offset)
		if err != nil {
			log.Fatalf("Error reading file: %v", err)
		}
		if i%10000 == 0 {
			log.Printf("Finished %v reads in serial", i)
		}
	}
	fmt.Println("Serial: Number of random reads per second: ",
		float64(*numReads)/time.Since(startT).Seconds())
	fmt.Println("Serial: Time Taken: ", time.Since(startT))
}

func Conc2(fList []*os.File, maxFileSize int64) {
	startT := time.Now()
	var i int64
	var wg sync.WaitGroup

	for k := 0; k < *numGoroutines; k++ {
		wg.Add(1)
		go func() {
			b := make([]byte, int(readSize))
			r := rand.New(rand.NewSource(time.Now().UnixNano()))
			for atomic.LoadInt64(&i) < *numReads {
				fd, offset := getIndices(r, fList, maxFileSize)
				_, err := fd.ReadAt(b, offset)
				if err != nil {
					log.Fatalf("Error reading file: %v", err)
				}
				atomic.AddInt64(&i, 1)
			}
			wg.Done()
		}()
	}
	wg.Wait()
	fmt.Println("Concurrent 2: Number of random reads per second: ",
		float64(*numReads)/time.Since(startT).Seconds())
	fmt.Println("Concurrent 2: Time Taken: ", time.Since(startT))
}

type req struct {
	fd     *os.File
	offset int64
}

func Conc3(fList []*os.File, maxFileSize int64) {
	ch := make(chan req, 10000)
	go func() {
		var i int64
		rd := rand.New(rand.NewSource(time.Now().UnixNano()))
		for i = 0; i < *numReads; i++ {
			var r req
			r.fd, r.offset = getIndices(rd, fList, maxFileSize)
			ch <- r
		}
		close(ch)
	}()

	startT := time.Now()
	var wg sync.WaitGroup
	for k := 0; k < *numGoroutines; k++ {
		wg.Add(1)
		go func() {
			b := make([]byte, int(readSize))
			for req := range ch {
				_, err := req.fd.ReadAt(b, req.offset)
				if err != nil {
					log.Fatalf("Error reading file: %v", err)
				}
			}
			wg.Done()
		}()
	}
	wg.Wait()
	fmt.Println("Concurrent 3: Number of random reads per second: ",
		float64(*numReads)/time.Since(startT).Seconds())
	fmt.Println("Concurrent 3: Time Taken: ", time.Since(startT))
}

func main() {
	flag.Parse()

	var flist []*os.File
	var maxFileSize int64
	getFile := func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Print(err)
			return nil
		}

		if !info.IsDir() {
			f, err := os.Open(path)
			y.AssertTruef(err == nil, "Error opening file: %v", path)
			flist = append(flist, f)
			log.Println("Opened file:", path, "Size:", info.Size()/(1<<20), "MB")
			maxFileSize = info.Size()
		}
		return nil
	}

	err := filepath.Walk(*dir, getFile)
	if err != nil {
		log.Fatalf("%v", err)
	}
	if len(flist) == 0 {
		log.Fatalf("Must have files already created")
	}

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			log.Fatal(err)
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	switch *mode {
	case 0:
		Serial(flist, maxFileSize)
	case 1:
		Conc2(flist, maxFileSize)
	case 2:
		Conc3(flist, maxFileSize)
	}
}