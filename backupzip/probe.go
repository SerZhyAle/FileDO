package backupzip

import (
	"compress/flate"
	"io"
	"os"
	"sort"
	"time"
)

// The density probe (SP-0198 spec 3.4, numbers frozen by the measurements of
// Q3): a bounded look at the content before packing decides the zip level.
const (
	ProbeFiles      = 24               // files sampled, the largest first, then spread
	ProbeReadCap    = 4 << 20          // bytes read from one sampled file
	ProbeSampleCap  = 48 << 20         // bytes in the whole sample
	ProbeDeadline   = 10 * time.Second // the owner's ten seconds, reads included
	ProbeMediaFloor = 3.0              // percent saved at level 6 under which the level is 0 (store)
	ProbeStepGain   = 1.0              // percentage points a higher level must add to be chosen
)

// probeLevels are the deflate levels the probe tries; 0 (store) is the floor.
var probeLevels = []int{1, 6, 9}

// ProbeResult is what the probe measured and what it chose.
type ProbeResult struct {
	Level       int
	Seconds     float64
	Files       int
	SampleBytes int64
	// Saving maps each level the probe finished to the percent it saved on
	// the sample. A level missing here was not reached before the deadline.
	Saving   map[int]float64
	TimedOut bool
}

// ChosenSaving is the saving of the chosen level on the sample, or the level-6
// saving that sent the content to store.
func (r ProbeResult) ChosenSaving() float64 {
	if r.Level == 0 {
		if s, ok := r.Saving[6]; ok {
			return s
		}
		return r.Saving[1]
	}
	return r.Saving[r.Level]
}

// probeClock is the probe's clock; a test replaces it.
var probeClock = time.Now

// Probe samples the files of w and picks the level. open reads one entry
// (nil opens Entry.Path); stop is asked between files and between levels.
func Probe(w Walked, open func(Entry) (io.ReadCloser, error), stop func() bool) (ProbeResult, error) {
	start := probeClock()
	deadline := start.Add(ProbeDeadline)
	if open == nil {
		open = func(e Entry) (io.ReadCloser, error) { return os.Open(e.Path) }
	}
	res := ProbeResult{Saving: map[int]float64{}}
	finish := func() ProbeResult {
		res.Seconds = probeClock().Sub(start).Seconds()
		res.Level = chooseLevel(res)
		return res
	}

	var sample [][]byte
	for _, e := range probeSample(w) {
		if stop != nil && stop() {
			return ProbeResult{}, ErrStopped
		}
		if !probeClock().Before(deadline) {
			res.TimedOut = true
			break
		}
		room := int64(ProbeSampleCap) - res.SampleBytes
		if room <= 0 {
			break
		}
		n := e.Size
		if n > ProbeReadCap {
			n = ProbeReadCap
		}
		if n > room {
			n = room
		}
		b, err := readHead(e, open, n)
		if err != nil {
			return ProbeResult{}, err
		}
		sample = append(sample, b)
		res.Files++
		res.SampleBytes += int64(len(b))
	}
	if res.SampleBytes == 0 {
		return finish(), nil
	}

	for _, level := range probeLevels {
		if stop != nil && stop() {
			return ProbeResult{}, ErrStopped
		}
		out, done, err := deflatedSize(sample, level, deadline, stop)
		if err != nil {
			return ProbeResult{}, err
		}
		if !done {
			res.TimedOut = true
			break
		}
		res.Saving[level] = 100 * (1 - float64(out)/float64(res.SampleBytes))
	}
	return finish(), nil
}

// chooseLevel is the rule of the measurements (Q3): store when the content
// does not compress, otherwise step up from level 1 while a step adds at least
// ProbeStepGain points. "Bytes saved per second" was measured and dropped: the
// fastest level always wins it, so it could never choose a denser one.
func chooseLevel(r ProbeResult) int {
	if r.SampleBytes == 0 {
		return 0
	}
	s1, ok1 := r.Saving[1]
	if !ok1 {
		// Nothing was measured before the deadline: deflate's own default.
		return 6
	}
	floor := s1
	if s6, ok := r.Saving[6]; ok {
		floor = s6
	}
	if floor < ProbeMediaFloor {
		return 0
	}
	level := 1
	if s6, ok := r.Saving[6]; ok && s6-s1 >= ProbeStepGain {
		level = 6
		if s9, ok := r.Saving[9]; ok && s9-s6 >= ProbeStepGain {
			level = 9
		}
	}
	return level
}

// probeSample picks at most ProbeFiles non-empty files: the eight largest,
// then an even spread over the rest, so one huge file and a crowd of small
// ones are both seen. A small tree is sampled whole.
func probeSample(w Walked) []Entry {
	var files []Entry
	for _, e := range w.Entries {
		if !e.Dir && e.Size > 0 {
			files = append(files, e)
		}
	}
	if len(files) <= ProbeFiles {
		return files
	}
	sort.SliceStable(files, func(i, j int) bool { return files[i].Size > files[j].Size })
	const largest = 8
	pick := append([]Entry(nil), files[:largest]...)
	rest := files[largest:]
	step := len(rest) / (ProbeFiles - largest)
	if step < 1 {
		step = 1
	}
	for i := 0; i < len(rest) && len(pick) < ProbeFiles; i += step {
		pick = append(pick, rest[i])
	}
	return pick
}

func readHead(e Entry, open func(Entry) (io.ReadCloser, error), n int64) ([]byte, error) {
	f, err := open(e)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b := make([]byte, n)
	got, err := io.ReadFull(f, b)
	if err == io.ErrUnexpectedEOF || err == io.EOF {
		// The file shrank since the walk; what is there is the sample.
		err = nil
	}
	if err != nil {
		return nil, err
	}
	return b[:got], nil
}

// countWriter counts what a deflater writes and keeps none of it.
type countWriter struct{ n int64 }

func (c *countWriter) Write(p []byte) (int, error) {
	c.n += int64(len(p))
	return len(p), nil
}

// deflatedSize compresses the whole sample as one stream at level and reports
// its size; done is false when the deadline came first.
func deflatedSize(sample [][]byte, level int, deadline time.Time, stop func() bool) (int64, bool, error) {
	var cw countWriter
	fw, err := flate.NewWriter(&cw, level)
	if err != nil {
		return 0, false, err
	}
	for _, b := range sample {
		if stop != nil && stop() {
			return 0, false, ErrStopped
		}
		if !probeClock().Before(deadline) {
			return 0, false, nil
		}
		if _, err := fw.Write(b); err != nil {
			return 0, false, err
		}
	}
	if err := fw.Close(); err != nil {
		return 0, false, err
	}
	return cw.n, true, nil
}
