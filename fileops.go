// Quiesce - Windows system cleaner and RAM optimizer.
// Copyright (C) 2026 SibtainOcn <https://github.com/SibtainOcn/Quiesce>
//
// This program is free software: you can redistribute it and/or modify it
// under the terms of the GNU General Public License as published by the Free
// Software Foundation, either version 3 of the License, or (at your option)
// any later version.
//
// This program is distributed in the hope that it will be useful, but WITHOUT
// ANY WARRANTY; without even the implied warranty of MERCHANTABILITY or
// FITNESS FOR A PARTICULAR PURPOSE. See the GNU General Public License for
// more details.
//
// You should have received a copy of the GNU General Public License along
// with this program. If not, see <https://www.gnu.org/licenses/>.

package main

// Shared file-deletion helpers. Any task that clears a folder - Windows
// temp, a browser cache, an app cache - uses these, so they all get the same
// parallelism, progress reporting, size accounting and pause/abort support.

import (
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
)

// deleteWorkers caps concurrent deletions, to avoid exhausting file handles.
const deleteWorkers = 32

// CleanDir deletes everything directly inside dir - files and whole
// subfolders - but keeps dir itself. It returns how many top-level items
// were removed and how many could not be (in use or protected).
func (tc *TaskCtx) CleanDir(dir string) (deleted, failed int64) {
	entries, err := os.ReadDir(dir)
	// ReadDir returns what it managed to read alongside an error, so a
	// partly readable folder is still cleaned.
	if err != nil && len(entries) == 0 {
		if !os.IsNotExist(err) {
			tc.Log(LogWarn, Tf("cleaner.read_fail", dir, err))
		}
		return 0, 0
	}

	tc.SetTotal(len(entries))
	var d, f atomic.Int64
	panics := tc.parallel(len(entries), func(i int) {
		e := entries[i]
		p := filepath.Join(dir, e.Name())

		if e.IsDir() {
			size := treeSize(p)
			if err := os.RemoveAll(p); err != nil {
				// Partly removed: count the space that was actually freed.
				tc.v.AddBytes(size - treeSize(p))
				f.Add(1)
				tc.v.Failed()
				return
			}
			d.Add(1)
			tc.v.Deleted(e.Name()+`\`, size)
			return
		}

		var size int64
		if info, err := e.Info(); err == nil {
			size = info.Size()
		}
		if err := os.Remove(p); err != nil {
			f.Add(1)
			tc.v.Failed()
			return
		}
		d.Add(1)
		tc.v.Deleted(e.Name(), size)
	})
	return d.Load(), f.Load() + panics
}

// CleanTree deletes every file anywhere under dir, leaving the folder
// structure in place. Used where a service expects its folders to exist.
func (tc *TaskCtx) CleanTree(dir string) (deleted, failed int64) {
	type item struct {
		path string
		size int64
	}
	var files []item

	err := filepath.WalkDir(dir, func(p string, de fs.DirEntry, err error) error {
		if err != nil {
			if p == dir {
				return err
			}
			return nil // unreadable subfolder: skip it, keep walking
		}
		if tc.Aborted() {
			return filepath.SkipAll
		}
		if de.IsDir() {
			return nil
		}
		var size int64
		if info, err := de.Info(); err == nil {
			size = info.Size()
		}
		files = append(files, item{p, size})
		return nil
	})
	if err != nil {
		if !os.IsNotExist(err) {
			tc.Log(LogWarn, Tf("cleaner.read_fail", dir, err))
		}
		return 0, 0
	}

	tc.SetTotal(len(files))
	var d, f atomic.Int64
	panics := tc.parallel(len(files), func(i int) {
		if err := os.Remove(files[i].path); err != nil {
			f.Add(1)
			tc.v.Failed()
			return
		}
		d.Add(1)
		tc.v.Deleted(filepath.Base(files[i].path), files[i].size)
	})
	return d.Load(), f.Load() + panics
}

// parallel calls fn(0..n-1) on up to deleteWorkers goroutines, stopping
// early on abort and waiting while paused. A panic in fn is contained and
// counted as a failure; the number of panics is returned.
func (tc *TaskCtx) parallel(n int, fn func(i int)) int64 {
	var (
		wg     sync.WaitGroup
		panics atomic.Int64
		sem    = make(chan struct{}, deleteWorkers)
	)
	for i := 0; i < n; i++ {
		if !tc.Checkpoint() {
			break
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			defer func() {
				if r := recover(); r != nil {
					panics.Add(1)
					tc.v.Failed()
				}
			}()
			fn(i)
		}(i)
	}
	wg.Wait()
	return panics.Load()
}

// treeSize sums the sizes of the files under p without following links.
// Errors are ignored: it is used for a "space freed" figure, not for safety.
func treeSize(p string) int64 {
	var total int64
	_ = filepath.WalkDir(p, func(_ string, de fs.DirEntry, err error) error {
		if err != nil || de.IsDir() {
			return nil
		}
		if info, err := de.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}
