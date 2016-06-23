package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type clip struct {
	Start float64
	End   float64
	Name  string
}

func (c *clip) String() string {
	return fmt.Sprintf("clip{%v, %v, %v}", c.Start, c.End, c.Name)
}

type command struct {
	Input string
	Clip  *clip
}

var (
	filenameSanitizer = regexp.MustCompile(`[\s\<\>:"/\\|\*\?]`)
)

func parseTimestamp(s string) float64 {
	s = regexp.MustCompile(`\s`).ReplaceAllString(s, "")
	if s == "" {
		return 0
	}
	fields := strings.SplitN(s, ":", 2)
	if len(fields) > 2 {
		log.Fatalf("error parsing timestamp %s", s)
	}
	if len(fields) == 1 {
		ss, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0
		}
		return ss
	}
	mm, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0
	}
	ss, err := strconv.ParseFloat(fields[1], 64)
	if err != nil {
		return 0
	}
	return mm*60 + ss
}

func parseClip(line string) *clip {
	fields := strings.SplitN(line, ",", 3)
	if len(fields) < 3 {
		return nil
	}
	c := &clip{
		Start: parseTimestamp(fields[0]),
		End:   parseTimestamp(fields[1]),
		Name:  fields[2],
	}
	if c.End < c.Start {
		c.End = c.Start + 1
	}
	return c
}

func parseClipFile(fname string) []*clip {
	rgx := regexp.MustCompile(`^\s*$`)
	f, err := os.Open(fname)
	if err != nil {
		log.Fatalln("could not open clip file")
	}
	buffered := bufio.NewReader(f)
	// skip first line
	buffered.ReadLine()
	var clips []*clip
	count := 1
	for {
		l, _, err := buffered.ReadLine()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatalf("could not read line #%d: %v", count, err)
		}
		str := rgx.ReplaceAllString(string(l), "")
		if str == "" {
			count++
			continue
		}
		clip := parseClip(str)
		if clip == nil {
			log.Fatalf("could not parse line #%d: %v", count, str)
		}
		clips = append(clips, clip)
		count++
	}
	return clips
}

func sanitizeName(s string) string {
	return filenameSanitizer.ReplaceAllString(s, "_")
}

func findNextName(s string) string {
	count := 1
	ext := filepath.Ext(s)
	s = strings.TrimSuffix(s, ext)
	testName := s + ext
	log.Printf("finding next name for %s", testName)
	for {
		log.Printf("trying %s", testName)
		_, err := os.Stat(testName)
		if err != nil && !os.IsNotExist(err) {
			log.Fatalln(err)
		}
		if os.IsNotExist(err) {
			log.Printf("found %s", testName)
			return testName
		}
		count++
		testName = fmt.Sprintf("%s%d%s", s, count, ext)
	}
}

func convert(input string, c *clip) {
	output := findNextName(c.Name + ".mp3")
	cmd := exec.Command("ffmpeg",
		"-nostats",
		"-nostdin",
		"-y",
		"-i", input,
		"-ss", strconv.FormatFloat(c.Start, 'f', -1, 64),
		"-to", strconv.FormatFloat(c.End, 'f', -1, 64),
		"-c:a", "libmp3lame",
		"-q", "9",
		"-v", "error",
		output)
	combOut, err := cmd.CombinedOutput()
	if err != nil {
		errStr := fmt.Errorf("error running ffmpeg: %v\n\n"+
			"the output of the command was\n\n"+
			"---------\n%s\n---------\n\n", err, string(combOut))
		log.Fatalln(errStr)
	}
}

func converter(cmds <-chan *command) {
	for c := range cmds {
		log.Printf("beginning convert for %v", c.Clip.Name)
		convert(c.Input, c.Clip)
	}
}

func unlinkIfExists(dir string) {
	removed := false
	for {
		_, err := os.Stat(dir)
		if err != nil {
			if !os.IsNotExist(err) {
				log.Fatalln(err)
			}
			return
		}
		if !removed {
			err = os.RemoveAll(dir)
			if err != nil {
				log.Fatalln(err)
			}
			removed = true
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func main() {
	if len(os.Args) < 3 {
		fmt.Printf("usage: %v clipfile.txt input.wav\n", os.Args[0])
		os.Exit(1)
	}
	clipsDir := strings.TrimSuffix(os.Args[1], ".txt")
	unlinkIfExists(clipsDir)
	err := os.Mkdir(clipsDir, os.ModeDir)
	if err != nil {
		log.Fatalln(err)
	}
	clips := parseClipFile(os.Args[1])
	cmds := make(chan *command)
	runtime.GOMAXPROCS(runtime.NumCPU())
	for i := 0; i < runtime.NumCPU(); i++ {
		go converter(cmds)
	}
	for _, clip := range clips {
		clip.Name = filepath.Join(clipsDir, sanitizeName(clip.Name))
		cmds <- &command{
			os.Args[2],
			clip,
		}
	}
	close(cmds)
}
