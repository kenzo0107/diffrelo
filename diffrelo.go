package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"github.com/udhos/equalfile"
)

const (
	version          = "v0.0.2"
	filelist         = "list.txt"
	remoteDir        = "remoteDir"
	localDir         = "localDir"
	stdSeparator     = "/"
	semaphoreDefault = 4
)

type strslice []string

func (s *strslice) String() string {
	return fmt.Sprintf("%v", *s)
}

func (s *strslice) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func main() {

	start := time.Now()

	// number of CPU Core.
	cpus := runtime.NumCPU()
	runtime.GOMAXPROCS(cpus)

	var exts strslice
	var vexts strslice
	var vdirs strslice
	var target string
	var showVersion bool
	var localWorkspace string
	var remoteWorkspace string
	var semaphoreCount int
	var input string
	var output string
	var skipTrailingCr bool

	flag.Var(&exts, "ext", "include file extension. default: php,tpl,js,css,html")
	flag.Var(&vexts, "vext", "exclude file extension. default: tpl.php,sql,tar.gz")
	flag.Var(&vdirs, "vdirs", "exclude directory extension. default: data/Smarty/templates_c")
	flag.StringVar(&target, "t", "", "target hostname")
	flag.StringVar(&remoteWorkspace, "r", "/var/www/html", "workspace in remote server")
	flag.StringVar(&localWorkspace, "l", "/Users/kenzo/go/src/github.com", "local workspace")
	flag.IntVar(&semaphoreCount, "sem", semaphoreDefault, "semaphore limit count for goroutine")
	flag.StringVar(&input, "in", "", "input file")
	flag.StringVar(&output, "out", "list.txt", "output file")
	flag.BoolVar(&skipTrailingCr, "skipeol", false, "ignore end of line")

	flag.BoolVar(&showVersion, "v", false, "show version")
	flag.Parse()

	if showVersion {
		fmt.Println("version:", version)
		return
	}

	if len(target) == 0 {
		err := errors.New("[error] You don't set target hostname ? Please set '-t' target hostname")
		log.Fatal(err)
	}

	if skipTrailingCr {
		if err := existCommand("diff"); err != nil {
			fmt.Println("command `diff` not found.")
			return
		}
	}

	// set exts default values.
	if len(exts) == 0 {
		exts = append(exts, "php")
		exts = append(exts, "tpl")
		exts = append(exts, "js")
		exts = append(exts, "css")
		exts = append(exts, "html")
	}

	// set vexts default values.
	if len(vexts) == 0 {
		vexts = append(vexts, "sql")
		vexts = append(vexts, "gz")
		vexts = append(vexts, "zip")
	}

	if len(vdirs) == 0 {
		vdirs = append(vdirs, "data/Smarty/templates_c")
		vdirs = append(vdirs, "data/class/api/operations")
		vdirs = append(vdirs, "data/class/api")
		vdirs = append(vdirs, "data/class_extends/api_extends")
		vdirs = append(vdirs, "data/downloads")
		vdirs = append(vdirs, "data/download")
		vdirs = append(vdirs, "data/module")
		vdirs = append(vdirs, "data/plugin")
		vdirs = append(vdirs, "data/smarty_extends")
	}

	cwd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}

	initialize(cwd)

	// command start.
	cmd := exec.Command("ssh", target, "-s", "sftp")

	// send errors from ssh to stderr
	cmd.Stderr = os.Stderr

	// get stdin and stdout
	wr, err := cmd.StdinPipe()
	if err != nil {
		fmt.Fprintln(os.Stderr, "[error]", err)
	}
	rd, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Fprintln(os.Stderr, "[error]", err)
	}

	// start the process
	if err = cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "[error]", err)
	}
	defer cmd.Wait()

	// open the SFTP session
	client, err := sftp.NewClientPipe(rd, wr)
	if err != nil {
		log.Fatal("[error]", err)
	}
	defer client.Close()

	var lines []string
	if len(input) > 0 {
		lines = getLinesFromFile(input)
	} else {
		lines = getLocalFilepathList(localWorkspace, exts, vexts, vdirs)
	}

	isMerge := true

	var writer *bufio.Writer

	if len(output) > 0 {
		f := newFile(output)
		defer f.Close()
		writer = bufio.NewWriter(f)
	}

	cmp := equalfile.New(nil, equalfile.Options{})

	// semaphore for concurrency goroutine limit.
	sem := make(chan struct{}, semaphoreCount)
	var wg sync.WaitGroup
	for _, l := range lines {
		wg.Add(1)

		go func(l string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			var equal bool

			splitpath := strings.Split(l, stdSeparator)

			localDst, localErr := setOneLocalFile(localWorkspace, splitpath)

			remoteDst, remoteErr := setOneRemoteFile(client, remoteWorkspace, splitpath)

			// Get difference between localfile and remotefile.
			if localErr == nil && remoteErr == nil {
				equal = false
				err = nil
				if skipTrailingCr {
					equal, err = isEqual(localDst, remoteDst)
				} else {
					equal, err = cmp.CompareFile(localDst, remoteDst)
				}

				if err != nil {
					fmt.Fprintf(os.Stderr, "[error] %v - %s\n", err, l)
				}

				if !equal {
					fmt.Fprintf(os.Stdout, "[diff] %s\n", l)
					isMerge = false

					if writer != nil {
						_, err := writer.WriteString(l + "\n")
						if err != nil {
							fmt.Fprintf(os.Stderr, "[error] %v - %s\n", err, l)
						}
					}
				}
			}

			if localErr == nil && remoteErr != nil {
				fmt.Fprintf(os.Stdout, "[new] %s\n", l)

				if writer != nil {
					_, err := writer.WriteString(l + "\n")
					if err != nil {
						fmt.Fprintf(os.Stderr, "[error] %v - %s\n", err, l)
					}
				}
			}

			if localErr != nil {
				fmt.Fprintf(os.Stdout, "[Not found] %s\n", localDst)
			}
		}(l)
	}
	wg.Wait()

	if writer != nil {
		writer.Flush()
		fmt.Fprintln(os.Stdout, "output a file named:", output)
	}

	if isMerge {
		fmt.Fprintln(os.Stdout, "[All files have merged] (^-^)/Bye")
	}

	elapsed := time.Since(start)
	fmt.Printf("Executed: %f sec \n", elapsed.Seconds())
}

// initialize ... Clean up remoteDir, localDir.
func initialize(cwd string) {

	r := filepath.Join(cwd, remoteDir)
	l := filepath.Join(cwd, localDir)

	if err := os.RemoveAll(r); err != nil {
		log.Fatal("[error]", err)
		return
	}
	if err := os.RemoveAll(l); err != nil {
		log.Fatal("[error]", err)
		return
	}

	if err := reMakeDir(r); err != nil {
		log.Fatal("[error]", err)
		return
	}
	if err := reMakeDir(l); err != nil {
		log.Fatal("[error]", err)
		return
	}
}

// getLocalFilepathList ... Get local file path by setting root.
func getLocalFilepathList(root string, exts, vexts, vdirs []string) (files []string) {

	if len(exts) > 0 {
		exts = addDot2PrefixInStringArray(exts)
	}

	if len(vexts) > 0 {
		vexts = addDot2PrefixInStringArray(vexts)
	}

	filepath.Walk(root,
		func(path string, info os.FileInfo, err error) error {

			if info.IsDir() {
				return nil
			}

			isIncludeFile := true

			rel, _ := filepath.Rel(root, path)

			ext := filepath.Ext(rel)

			// var dir = filepath.Dir(path)
			// var relPath = dir[len(root)+1:]
			// if isExist, _ := inStringContainArray(relPath, vdirs); isExist {
			// 	isIncludeFile = false
			// }

			if len(exts) > 0 {
				if isExist, _ := inStringArray(ext, exts); !isExist {
					isIncludeFile = false
				}
			}
			if len(vexts) > 0 {
				if isExist, _ := inStringArray(ext, vexts); isExist {
					isIncludeFile = false
				}
			}

			if isIncludeFile {
				// fmt.Printf("rel: %s, path: %s, ext: %s\n", rel, path, ext)
				files = append(files, rel)
			}

			return nil
		})

	return files
}

// setOneLocalFile ... move a file on local workspace to directory for local file.
func setOneLocalFile(localWorkspace string, splitpath []string) (localDst string, err error) {

	localSrc := getPath(localWorkspace, splitpath)
	localDst = getPath(localDir, splitpath)

	if !exists(localSrc) {
		err = fmt.Errorf("Not found File %s", localSrc)
		return
	}

	localDstDir := filepath.Dir(localDst)
	if !exists(localDstDir) {
		if err = reMakeDir(localDstDir); err != nil {
			return
		}
	}

	err = copy(localDst, localSrc)
	if err != nil {
		return
	}

	return
}

// setOneLocalFile ... Download a file from remote sever, move the file to directory for remote file.
func setOneRemoteFile(client *sftp.Client, remoteWorkspace string, splitpath []string) (remoteDst string, err error) {

	remoteSrc := getPath(remoteWorkspace, splitpath)
	remoteDst = getPath(remoteDir, splitpath)

	_, err = client.Stat(remoteSrc)
	if err != nil {
		return remoteDst, err
	}

	// Open the source file
	remoteSrcFile, err := client.Open(remoteSrc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[error] clien.Open failed %v - %s\n", err, remoteSrc)
		return
	}
	defer remoteSrcFile.Close()

	// Create the destination Directory.
	remoteDstDir := filepath.Dir(remoteDst)
	if !exists(remoteDstDir) {
		if err = reMakeDir(remoteDstDir); err != nil {
			return
		}
	}

	// Create the destination file.
	remoteDstFile, err := os.Create(remoteDst)
	if err != nil {
		return
	}
	defer remoteDstFile.Close()

	// Download.
	remoteSrcFile.WriteTo(remoteDstFile)
	return
}

// getPath ... get path by filepath join method.
func getPath(workspace string, splitpath []string) string {
	s := []string{}
	s = append(s, workspace)
	s = append(s, splitpath...)
	src := filepath.Join(s...)
	return src
}

// reMakeDir ... Make a directory when it doesn't exist.
func reMakeDir(dir string) (e error) {

	if e = os.RemoveAll(dir); e != nil {
		fmt.Fprintln(os.Stderr, "[error]", e)
		return
	}

	e = os.MkdirAll(dir, 0755)
	if e != nil {
		fmt.Fprintln(os.Stderr, "[error]", e)
		return
	}
	return
}

// exists ... Check wether a file exists.
func exists(f string) bool {
	_, e := os.Stat(f)
	return e == nil
}

// getLinesFromFile ... Get lines from a text file.
func getLinesFromFile(path string) []string {
	// open file.
	f, e := os.Open(path)
	if e != nil {
		fmt.Fprintf(os.Stderr, "File %s could not read: %v\n", path, e)
		os.Exit(1)
	}
	defer f.Close()

	l := make([]string, 0, 100)
	s := bufio.NewScanner(f)
	for s.Scan() {
		l = append(l, s.Text())
	}
	if se := s.Err(); se != nil {
		fmt.Fprintf(os.Stderr, "File %s scan error: %v\n", path, e)
	}

	return l
}

// copy ... Copy src to dst.
func copy(dst, src string) (e error) {
	i, e := os.Open(src)
	if e != nil {
		return
	}
	defer i.Close()

	o, e := os.Create(dst)
	if e != nil {
		return
	}
	defer o.Close()

	_, e = io.Copy(o, i)
	if e != nil {
		return
	}

	e = o.Sync()
	if e != nil {
		return
	}
	return
}

// inStringArray ... Return true, index >= 0 if specific string value is in string array, Else false, index = -1.
func inStringArray(v string, arr []string) (b bool, i int) {

	b = false
	i = -1

	for k, s := range arr {
		if v == s {
			i = k
			b = true
			return
		}
	}

	return
}

func inStringContainArray(v string, arr []string) (b bool, i int) {

	b = false
	i = -1

	for k, s := range arr {
		if x := strings.Index(s, v); x != i {
			i = k
			b = true
			return
		}
	}
	return
}

// addDot2Prefix ... Add dot "." to prefix.
func addDot2PrefixInStringArray(s []string) (t []string) {
	for _, x := range s {
		y := "." + x
		t = append(t, y)
	}
	return t
}

// newFile ... Create new file.
func newFile(fn string) *os.File {
	fp, err := os.Create(fn)
	if err != nil {
		log.Fatal(err)
	}
	return fp
}

func existCommand(cmd string) (e error) {
	c := fmt.Sprintf("which %s", cmd)
	e = exec.Command("sh", "-c", c).Run()
	return
}

func isEqual(f1, f2 string) (b bool, e error) {

	b = false

	c := fmt.Sprintf("diff --strip-trailing-cr -r %s %s | wc -l | awk '{if($1!=0) print 'diff'}'", f1, f2)

	var o []byte
	o, e = exec.Command("sh", "-c", c).Output()
	if e != nil {
		return
	}

	if len(o) == 0 {
		b = true
		return
	}

	return
}
