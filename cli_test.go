package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseRunArgs(t *testing.T) {
	got, err := ParseRunArgs([]string{"TEMP-1", "deep", "bin", "temp-1", " wu ", ""})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"temp-1", "deep", "bin", "win-update"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	if got, err := ParseRunArgs(nil); err != nil || got != nil {
		t.Errorf("no args: got %v, %v", got, err)
	}

	for _, bad := range [][]string{{"temp-3"}, {"temp", "nope"}, {"ram-5"}, {"--temp"}, {"temp", "update"}} {
		if _, err := ParseRunArgs(bad); err == nil {
			t.Errorf("%v: expected an error", bad)
		}
	}
}

func TestBuildRunConfigOnlyListedSteps(t *testing.T) {
	saved := DefaultConfig()
	saved.KeepLog = 1
	cfg := BuildRunConfig(saved, []string{"temp-2", "bin", "deep"})

	for i := 1; i <= 11; i++ {
		want := 0
		if i == 2 || i == 11 {
			want = 1
		}
		if cfg.GetVal(i) != want {
			t.Errorf("step %d = %d, want %d", i, cfg.GetVal(i), want)
		}
	}
	if cfg.DeepCleanup != 1 || cfg.KeepLog != 1 {
		t.Errorf("deep=%d keepLog=%d", cfg.DeepCleanup, cfg.KeepLog)
	}
	if saved.WinTemp != 1 || saved.RecycleBin != 0 {
		t.Error("saved config was modified")
	}
}

func TestBuildRunConfigTempAndRam(t *testing.T) {
	cfg := BuildRunConfig(DefaultConfig(), []string{"temp", "ram"})
	if cfg.WinTemp != 1 || cfg.UserTemp != 1 || cfg.RamOptimize != 1 {
		t.Fatal("temp/ram not enabled")
	}
	for _, i := range ramSubOptions {
		if cfg.GetVal(i) != 1 {
			t.Errorf("ram sub %d is off", i)
		}
	}

	cfg = BuildRunConfig(DefaultConfig(), []string{"ram-3"})
	if cfg.RamOptimize != 1 || cfg.RamFileCache != 1 ||
		cfg.RamFlushModified+cfg.RamPurgeStandby+cfg.RamTrimWorkingSets != 0 {
		t.Errorf("ram-3 gave %+v", cfg)
	}
}

func TestBuildRunConfigAll(t *testing.T) {
	saved := DefaultConfig()
	saved.Prefetch = 0
	cfg := BuildRunConfig(saved, []string{"all"})
	if cfg.Prefetch != 0 || cfg.WinTemp != 1 || cfg.DeepCleanup != 0 {
		t.Errorf("all should keep saved steps: %+v", cfg)
	}

	cfg = BuildRunConfig(saved, []string{"all", "deep", "ram-4"})
	if cfg.DeepCleanup != 1 || cfg.RamTrimWorkingSets != 1 || cfg.RamPurgeStandby != 0 || cfg.WinTemp != 1 {
		t.Errorf("all + extras: %+v", cfg)
	}
}

func TestSplitAttachArgs(t *testing.T) {
	pid, rest, found, err := splitAttachArgs([]string{"--attach", "1234", "dns", "bin"})
	if err != nil || !found || pid != 1234 || !reflect.DeepEqual(rest, []string{"dns", "bin"}) {
		t.Errorf("got %d %v %v %v", pid, rest, found, err)
	}
	if _, rest, found, _ := splitAttachArgs([]string{"dns"}); found || len(rest) != 1 {
		t.Error("plain args treated as attach")
	}
	for _, bad := range [][]string{{"--attach"}, {"--attach", "12"}, {"--attach", "x", "dns"}, {"--attach", "0", "dns"}} {
		if _, _, _, err := splitAttachArgs(bad); err == nil {
			t.Errorf("%v: expected an error", bad)
		}
	}
}

func TestRunTasksHideSkipped(t *testing.T) {
	forcePlainView = true
	ran := 0
	run := func(*TaskCtx) TaskOutcome { ran++; return TaskOutcome{} }
	tasks := []Task{{Name: "a"}, {Name: "b", Enabled: true, Run: run}, {Name: "c"}, {Name: "d", Enabled: true, Run: run}}
	rep := RunTasks(tasks, RunOptions{HideSkipped: true})
	if ran != 2 || !reflect.DeepEqual(rep.Ran, []bool{false, true, false, true}) {
		t.Errorf("ran=%d rep.Ran=%v", ran, rep.Ran)
	}
}

func TestDirectSummaryShowsOnlyChosenSteps(t *testing.T) {
	cfg := BuildRunConfig(DefaultConfig(), []string{"ram"})
	var b strings.Builder
	WriteSummary(&b, cfg, StepResults{})
	out := b.String()
	if !strings.Contains(stripANSI(out), "[1]  "+T("step.10.short")) {
		t.Error("chosen step missing")
	}
	for _, s := range []string{"[2]", "[10]", "[12]", T("common.skipped"), stripANSI(Tf("summary.total", 0))} {
		if strings.Contains(stripANSI(out), s) {
			t.Errorf("summary should not contain %q", s)
		}
	}

	cfg = BuildRunConfig(DefaultConfig(), []string{"dns", "bin"})
	b.Reset()
	WriteSummary(&b, cfg, StepResults{})
	plain := stripANSI(b.String())
	if !strings.Contains(plain, "[1]  "+T("step.9.short")) || !strings.Contains(plain, "[2]  "+T("step.11.short")) {
		t.Errorf("direct rows should be numbered from 1:\n%s", plain)
	}
	if strings.Contains(plain, Repo) {
		t.Error("summary should not show the repo URL")
	}

	cfg = BuildRunConfig(DefaultConfig(), []string{"temp-1"})
	b.Reset()
	WriteSummary(&b, cfg, StepResults{})
	if !strings.Contains(stripANSI(b.String()), stripANSI(Tf("summary.total", 0))) {
		t.Error("file step run should show totals")
	}
}

func TestEveryCommandHasStep(t *testing.T) {
	for _, c := range cliCommands {
		cfg := BuildRunConfig(DefaultConfig(), []string{c.name})
		on := c.step == 12 && cfg.DeepCleanup == 1 || c.step != 12 && cfg.GetVal(c.step) == 1
		if !on {
			t.Errorf("%s does not enable step %d", c.name, c.step)
		}
	}
}
