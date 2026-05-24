package jenkins

import "testing"

func TestJobURLPath(t *testing.T) {
	tests := []struct {
		job  string
		want string
	}{
		{"my-pipeline", "/job/my-pipeline"},
		{"team/my-pipeline", "/job/team/job/my-pipeline"},
		{"team/android/my-pipeline", "/job/team/job/android/job/my-pipeline"},
		{"/team/android/my-pipeline/", "/job/team/job/android/job/my-pipeline"},
	}
	for _, tc := range tests {
		got := jobURLPath(tc.job)
		if got != tc.want {
			t.Errorf("jobURLPath(%q) = %q; want %q", tc.job, got, tc.want)
		}
	}
}

func TestJobConsolePath(t *testing.T) {
	got := jobConsolePath("team/ci", 42)
	want := "/job/team/job/ci/42/consoleText"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
