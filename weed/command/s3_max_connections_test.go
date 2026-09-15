package command

import "testing"

func TestResolveMaxConnections(t *testing.T) {
	fileLimit := 64
	byteLimit := 2048
	minusOne := -1
	explicit := 500

	cases := []struct {
		name string
		opt  S3Options
		want int
	}{
		{
			name: "unlimited",
			opt:  S3Options{maxConnections: &minusOne},
			want: -1,
		},
		{
			name: "explicit",
			opt:  S3Options{maxConnections: &explicit},
			want: 500,
		},
		{
			name: "auto from file limit",
			opt:  S3Options{concurrentFileUploadLimit: &fileLimit},
			want: 2048, // 64*32
		},
		{
			name: "auto from byte limit only",
			opt:  S3Options{concurrentUploadLimitMB: &byteLimit},
			want: 1024,
		},
		{
			name: "no limits",
			opt:  S3Options{},
			want: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.opt.resolveMaxConnections(); got != tc.want {
				t.Fatalf("resolveMaxConnections()=%d want %d", got, tc.want)
			}
		})
	}
}
