//go:build windows

package main

import (
	"errors"
	"os/exec"
	"reflect"
	"testing"
)

func TestVD_AutoTasksFolderQuery(t *testing.T) {
	missing := &exec.ExitError{}
	denied := errors.New("access denied")
	for _, tt := range []struct {
		name string
		csv  string
		err  error
		want map[string]bool
	}{
		{"folder", "\"\\FileDO\\FileDO Mount Work\",\"N/A\",\"Ready\"\r\n" +
			"\"\\filedo\\FileDO Mount backup\",\"N/A\",\"Disabled\"\r\n" +
			"\"\\FileDO\\FileDO Shutdown Guard\",\"N/A\",\"Ready\"\r\n" +
			"\"\\Other\\FileDO Mount outside\",\"N/A\",\"Ready\"\r\n" +
			"\"\\FileDO\\Child\\FileDO Mount nested\",\"N/A\",\"Ready\"\r\n" +
			"\"\\FileDO2\\FileDO Mount similar\",\"N/A\",\"Ready\"\r\n",
			nil, map[string]bool{"work": true, "backup": true}},
		{"empty", "", nil, map[string]bool{}},
		{"missing folder exit", "", missing, map[string]bool{}},
		{"query failure", "", denied, map[string]bool{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := vdQueryAutoTasks(func(args ...string) ([]byte, error) {
				want := []string{"/Query", "/TN", vdTaskFolder, "/FO", "CSV", "/NH"}
				if !reflect.DeepEqual(args, want) {
					t.Fatalf("query arguments: got %q, want %q", args, want)
				}
				return []byte(tt.csv), tt.err
			})
			if err != tt.err || !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, %v; want %v, %v", got, err, tt.want, tt.err)
			}
			// Displays remain best effort even when the folder does not exist;
			// auto off still receives the query error and cannot claim removal.
			old := vdAutoTasksQuery
			t.Cleanup(func() { vdAutoTasksQuery = old })
			vdAutoTasksQuery = func() (map[string]bool, error) { return got, err }
			if tasks := vdAutoTasks(); !reflect.DeepEqual(tasks, tt.want) {
				t.Fatalf("display tasks: got %v, want %v", tasks, tt.want)
			}
		})
	}
}
