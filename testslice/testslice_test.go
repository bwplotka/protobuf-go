package slice_test

import (
	"syscall"
	"testing"

	tpb "google.golang.org/protobuf/internal/testprotos/test"
	"google.golang.org/protobuf/proto"
)

func BenchmarkMarshalSlice(b *testing.B) {
	testmsg := &tpb.TestAllTypes{}
	testmsg.RepeatedString = []string{
		"foo",
		"bar",
		"a longer string that exceeds what people would call a short message",
		"something inbetween",
	}

	var before syscall.Rusage
	max := proto.Size(testmsg)
	buf := make([]byte, 0, max)
	syscall.Getrusage(syscall.RUSAGE_SELF, &before)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := (proto.MarshalOptions{}).MarshalAppend(buf[:0], testmsg); err != nil {
			b.Fatalf("can't marshal message %+v: %v", testmsg, err)
		}
	}
	b.StopTimer()
	var after syscall.Rusage
	syscall.Getrusage(syscall.RUSAGE_SELF, &after)
	b.ReportMetric(float64(after.Utime.Nano()-before.Utime.Nano())/float64(b.N), "user-ns/op")
}
