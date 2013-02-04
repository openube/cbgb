package main

import (
	"flag"
	"log"
	"time"

	"github.com/couchbaselabs/cbgb"
)

var mutationLogCh = make(chan interface{})

func main() {
	addr := flag.String("bind", ":11211", "memcached listen port")
	data := flag.String("data", "./tmp", "data directory")
	defaultBucketName := flag.String("default-bucket-name",
		cbgb.DEFAULT_BUCKET_NAME, "name of the default bucket")
	flushInterval := flag.Int("flush-interval",
		10, "seconds between flushing or persisting mutations to storage")
	sleepInterval := flag.Int("sleep-interval",
		100, "seconds until files are closed (to be reopened on the next request)")
	compactInterval := flag.Int("compact-interval",
		10 * 60, "seconds until files are compacted")
	purgeTimeout := flag.Int("purge-timeout",
		10, "seconds until unused files are purged after compaction")

	flag.Parse()

	go cbgb.MutationLogger(mutationLogCh)

	buckets, err := cbgb.NewBuckets(*data,
		&cbgb.BucketSettings{
			FlushInterval:   time.Second * time.Duration(*flushInterval),
			SleepInterval:   time.Second * time.Duration(*sleepInterval),
			CompactInterval: time.Second * time.Duration(*compactInterval),
			PurgeTimeout:    time.Second * time.Duration(*purgeTimeout),
		})
	if err != nil {
		log.Fatalf("Could not make buckets: %v, data directory: %v", err, *data)
	}

	err = buckets.Load()
	if err != nil {
		log.Printf("Could not load buckets: %v, data directory: %v", err, *data)
	}

	if buckets.Get(*defaultBucketName) == nil {
		defaultBucket, err := buckets.New(*defaultBucketName)
		if err != nil {
			log.Fatalf("Error creating default bucket: %s, %v", *defaultBucketName, err)
		}

		defaultBucket.Subscribe(mutationLogCh)
		defaultBucket.CreateVBucket(0)
		defaultBucket.SetVBState(0, cbgb.VBActive)
	}

	if _, err := cbgb.StartServer(*addr, buckets, *defaultBucketName); err != nil {
		log.Fatalf("Error starting server: %s", err)
	}

	// Let goroutines do their work.
	select {}
}
