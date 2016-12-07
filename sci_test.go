package scipipe

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	//"os"
	"os"
	t "testing"
	"time"
)

func initTestLogs() {
	//InitLogDebug()
	InitLogWarning()
}

func TestBasicRun(t *t.T) {
	initTestLogs()

	t1 := NewFromShell("t1", "echo foo > {o:foo}")
	assert.IsType(t, t1.Out["foo"], NewFilePort())
	t1.PathFormatters["foo"] = func(t *SciTask) string {
		return "foo.txt"
	}

	t2 := NewFromShell("t2", "sed 's/foo/bar/g' {i:foo} > {o:bar}")
	assert.IsType(t, t2.In["foo"], NewFilePort())
	assert.IsType(t, t2.Out["bar"], NewFilePort())
	t2.PathFormatters["bar"] = func(t *SciTask) string {
		return t.GetInPath("foo") + ".bar.txt"
	}
	snk := NewSink()

	t2.In["foo"].Connect(t1.Out["foo"])
	snk.Connect(t2.Out["bar"])

	assert.IsType(t, t2.In["foo"], NewFilePort())
	assert.IsType(t, t2.Out["bar"], NewFilePort())

	pl := NewPipelineRunner()
	pl.AddProcesses(t1, t2, snk)
	pl.Run()

	cleanFiles("foo.txt", "foo.txt.bar.txt")
}

func TestParameterCommand(t *t.T) {
	initTestLogs()

	cmb := NewCombinatoricsProcess()

	// An abc file printer
	abc := NewFromShell("abc", "echo {p:a} {p:b} {p:c} > {o:out}")
	abc.PathFormatters["out"] = func(task *SciTask) string {
		return fmt.Sprintf(
			"%s_%s_%s.txt",
			task.Params["a"],
			task.Params["b"],
			task.Params["c"],
		)
	}

	// A printer process
	prt := NewFromShell("prt", "cat {i:in} >> /tmp/log.txt; rm {i:in}")

	// Connection info
	abc.ParamPorts["a"].Connect(cmb.A)
	abc.ParamPorts["b"].Connect(cmb.B)
	abc.ParamPorts["c"].Connect(cmb.C)
	prt.In["in"].Connect(abc.Out["out"])

	pl := NewPipelineRunner()
	pl.AddProcesses(cmb, abc, prt)
	pl.Run()

	// Run tests
	_, err := os.Stat("/tmp/log.txt")
	assert.Nil(t, err)

	cleanFiles("/tmp/log.txt")
}

func TestProcessWithoutInputsOutputs(t *t.T) {
	initTestLogs()
	Debug.Println("Starting test TestProcessWithoutInputsOutputs")

	f := "/tmp/hej.txt"
	tsk := NewFromShell("tsk", "echo hej > "+f)
	tsk.Run()
	_, err := os.Stat(f)
	assert.Nil(t, err, fmt.Sprintf("File is missing: %s", f))
	cleanFiles(f)
}

func TestDontOverWriteExistingOutputs(t *t.T) {
	InitLogError()
	Debug.Println("Starting test TestDontOverWriteExistingOutputs")

	f := "/tmp/hej.txt"

	// Assert file does not exist before running
	_, e1 := os.Stat(f)
	assert.NotNil(t, e1)

	// Run pipeline a first time
	tsk := NewFromShell("tsk", "echo hej > {o:hej1}")
	tsk.PathFormatters["hej1"] = func(task *SciTask) string { return f }

	prt := NewFromShell("prt", "echo {i:in1} Done!")
	prt.In["in1"].Connect(tsk.Out["hej1"])

	pl := NewPipelineRunner()
	pl.AddProcesses(tsk, prt)
	pl.Run()

	// Assert file DO exist after running
	fiBef, e2 := os.Stat(f)
	assert.Nil(t, e2)

	// Get modified time before
	mtBef := fiBef.ModTime()

	// Make sure some time has passed before the second write
	time.Sleep(1 * time.Millisecond)

	Debug.Println("Try running the same workflow again ...")
	// Run again with different output
	tsk = NewFromShell("tsk", "echo hej > {o:hej2}")
	tsk.PathFormatters["hej2"] = func(task *SciTask) string { return f }

	prt = NewFromShell("prt", "echo {i:in2} Done!")
	prt.In["in2"].Connect(tsk.Out["hej2"])

	pl = NewPipelineRunner()
	pl.AddProcesses(tsk, prt)
	pl.Run()

	// Assert exists
	fiAft, e3 := os.Stat(f)
	assert.Nil(t, e3)

	// Get modified time AFTER second run
	mtAft := fiAft.ModTime()

	// Assert file is not modified!
	assert.EqualValues(t, mtBef, mtAft)

	cleanFiles(f)
}

// Make sure that outputs are returned in order, even though they are
// spawned to work in parallel.
func TestSendsOrderedOutputs(t *t.T) {
	initTestLogs()

	fnames := []string{}
	for i := 1; i <= 10; i++ {
		fnames = append(fnames, fmt.Sprintf("/tmp/f%d.txt", i))
	}

	fq := NewFileQueue(fnames...)

	fc := NewFromShell("fc", "echo {i:in} > {o:out}")
	sl := NewFromShell("sl", "cat {i:in} > {o:out}")

	fc.PathFormatters["out"] = func(task *SciTask) string { return task.GetInPath("in") }
	sl.PathFormatters["out"] = func(task *SciTask) string { return task.GetInPath("in") + ".copy.txt" }

	fc.In["in"].Connect(fq.Out)
	sl.In["in"].Connect(fc.Out["out"])
	sl.Out["out"].Chan = make(chan *FileTarget, BUFSIZE)

	go fq.Run()
	go fc.Run()
	go sl.Run()

	assert.NotEmpty(t, sl.Out)

	var expFname string
	i := 1
	for ft := range sl.Out["out"].Chan {
		expFname = fmt.Sprintf("/tmp/f%d.txt.copy.txt", i)
		assert.EqualValues(t, expFname, ft.GetPath())
		i++
	}
	expFnames := []string{}
	for i := 1; i <= 10; i++ {
		expFnames = append(expFnames, fmt.Sprintf("/tmp/f%d.txt.copy.txt", i))
	}
	cleanFiles(fnames...)
	cleanFiles(expFnames...)
}

// Test that streaming works
func TestStreaming(t *t.T) {
	InitLogWarning()

	// Init processes
	ls := NewFromShell("ls", "ls -l / > {os:lsl}")
	ls.PathFormatters["lsl"] = func(task *SciTask) string {
		return "/tmp/lsl.txt"
	}
	grp := NewFromShell("grp", "grep etc {i:in} > {o:grepped}")
	grp.PathFormatters["grepped"] = func(task *SciTask) string {
		return task.GetInPath("in") + ".grepped.txt"
	}
	snk := NewSink()

	// Connect
	grp.In["in"].Connect(ls.Out["lsl"])
	snk.Connect(grp.Out["grepped"])

	// Run
	pl := NewPipelineRunner()
	pl.AddProcesses(ls, grp, snk)
	pl.Run()

	// Assert that a file exists
	_, err1 := os.Stat("/tmp/lsl.txt.fifo")
	assert.Nil(t, err1, "FIFO file does not exist, which it should!")

	// Assert otuput file exists
	_, err2 := os.Stat("/tmp/lsl.txt.grepped.txt")
	assert.Nil(t, err2, "File missing!")

	// Clean up
	cleanFiles("/tmp/lsl.txt", "/tmp/lsl.txt.grepped.txt")
	// cleanFiles("/tmp/lsl.txt.tmp")             // FIXME: Remove
	// cleanFiles("/tmp/lsl.txt.grepped.txt.tmp") // FIXME: Remove
	cleanFiles("/tmp/lsl.txt.fifo")
}

func TestGlobOutputs(t *t.T) {
	piperun := NewPipelineRunner()

	create := NewFromShell("create", "ls -l / > {o:out}")
	create.SetPathStatic("out", "ls.txt")
	piperun.AddProcess(create)

	split := NewFromShell("split", "split -l1 {i:in} ls.txt.split_ # {o:splits}")
	split.GlobOutputs("splits", "ls.txt.split_*")
	piperun.AddProcess(split)

	copyf := NewFromShell("copy", "cp {i:in} {o:out}")
	copyf.SetPathExtend("in", "out", ".copy")
	piperun.AddProcess(copyf)

	snk := NewSink()
	piperun.AddProcess(snk)

	create.Out["out"].Connect(split.In["in"])
	split.Out["splits"].Connect(copyf.In["in"])

	snk.Connect(copyf.Out["out"])

	piperun.Run()
}

// Helper processes

type CombinatoricsProcess struct {
	Process
	A *ParamPort
	B *ParamPort
	C *ParamPort
}

func NewCombinatoricsProcess() *CombinatoricsProcess {
	return &CombinatoricsProcess{
		A: NewParamPort(),
		B: NewParamPort(),
		C: NewParamPort(),
	}
}

func (proc *CombinatoricsProcess) Run() {
	defer proc.A.Close()
	defer proc.B.Close()
	defer proc.C.Close()

	for _, a := range []string{"a1", "a2", "a3"} {
		for _, b := range []string{"b1", "b2", "b3"} {
			for _, c := range []string{"c1", "c2", "c3"} {
				proc.A.Chan <- a
				proc.B.Chan <- b
				proc.C.Chan <- c
			}
		}
	}
}

func (proc *CombinatoricsProcess) IsConnected() bool { return true }

// Helper functions
func cleanFiles(fileNames ...string) {
	Debug.Println("Starting to remove files:", fileNames)
	for _, fileName := range fileNames {
		if _, err := os.Stat(fileName); err == nil {
			os.Remove(fileName)
			Debug.Println("Successfully removed file", fileName)
		}
	}
}
