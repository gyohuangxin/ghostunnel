/*-
 * Copyright 2015 Square Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io/ioutil"
	"path"
	"time"

	"gopkg.in/fsnotify.v1"
)

// Watch files using inotify/fswatch.
func watchAuto(files []string, notify chan bool) {
	hashes := hashFiles(files)
	watcher, err := fsnotify.NewWatcher()
	panicOnError(err)

	for _, file := range files {
		// Need to watch both directory and file, because we want to detect
		// files being overwritten (gives Write event) but also files being
		// removed/re-added.
		watcher.Add(file)
		watcher.Add(path.Dir(file))
	}

	for {
		select {
		case event := <-watcher.Events:
			for _, file := range files {
				name := path.Base(event.Name)
				if name == path.Base(file) {
					logger.Printf("received fs event for %s", name)

					// If we get Create event, it's probably because the file was
					// removed and then re-added. Need to re-register for events
					// on file or we won't get them in the future.
					if event.Op&fsnotify.Create == fsnotify.Create {
						watcher.Add(file)
					}

					if fileChanged(hashes, file) {
						logger.Printf("detected change on %s", name)
						notify <- true
					} else {
						logger.Printf("no change on %s", name)
					}

					break
				}
			}

		case err := <-watcher.Errors:
			logger.Printf("error watching file: %s", err)
		}
	}
}

// Watch files with a periodic timer, for filesystems that don't do
// inotify correctly (e.g. some fuse filesystems or other custom stuff).
func watchTimed(files []string, duration time.Duration, notify chan bool) {
	hashes := hashFiles(files)
	ticker := time.Tick(duration)

	for {
		<-ticker
		logger.Printf("running timed reload (timer fired)")

		change := false
		for _, file := range files {
			if fileChanged(hashes, file) {
				logger.Printf("detected change on %s, reloading", path.Base(file))
				change = true
			}
		}

		if change {
			notify <- true
		} else {
			logger.Printf("nothing changed, not reloading")
		}
	}
}

// Hash initial state of files we're watching
func hashFiles(files []string) map[string][32]byte {
	hashes := make(map[string][32]byte)

	for _, file := range files {
		hash, err := hashFile(file)
		if err != nil {
			logger.Printf("error reading file: %s", err)
			continue
		}

		name := path.Base(file)
		logger.Printf("sha256(%s) = %s", name, hex.EncodeToString(hash[:]))
		hashes[name] = hash
	}

	return hashes
}

// Read & hash a single file
func hashFile(file string) ([32]byte, error) {
	data, err := ioutil.ReadFile(file)
	if err != nil {
		return [32]byte{}, err
	}

	return sha256.Sum256(data), nil
}

// Check if a file has changed contents, update hash
func fileChanged(hashes map[string][32]byte, file string) bool {
	newHash, err := hashFile(file)
	if err != nil {
		logger.Printf("error reading file: %s", err)
		return false
	}

	name := path.Base(file)
	oldHash := hashes[name]
	if !bytes.Equal(oldHash[:], newHash[:]) {
		logger.Printf("sha256(%s) = %s", name, hex.EncodeToString(newHash[:]))
		hashes[name] = newHash
		return true
	}

	return false
}
