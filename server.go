package cbgb

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net"

	"github.com/dustin/gomemcached"
	"github.com/dustin/gomemcached/server"
)

const (
	VERSION = "0.0.0"
)

type reqHandler struct {
	buckets       *Buckets
	currentBucket Bucket
}

func (rh *reqHandler) HandleMessage(w io.Writer, req *gomemcached.MCRequest) *gomemcached.MCResponse {
	switch req.Opcode {
	case gomemcached.QUIT:
		return &gomemcached.MCResponse{
			Fatal: true,
		}
	case gomemcached.VERSION:
		return &gomemcached.MCResponse{
			Body: []byte(VERSION),
		}
	case gomemcached.NOOP:
		return &gomemcached.MCResponse{}
	case gomemcached.SASL_LIST_MECHS:
		if req.VBucket != 0 || req.Cas != 0 ||
			len(req.Key) != 0 || len(req.Extras) != 0 || len(req.Body) != 0 {
			return &gomemcached.MCResponse{
				Status: gomemcached.EINVAL,
			}
		}
		return &gomemcached.MCResponse{
			Body: []byte("PLAIN"),
		}
	case gomemcached.SASL_AUTH:
		if req.VBucket != 0 || req.Cas != 0 ||
			len(req.Extras) != 0 || len(req.Body) < 2 {
			return &gomemcached.MCResponse{
				Status: gomemcached.EINVAL,
			}
		}
		if !bytes.Equal(req.Key, []byte("PLAIN")) {
			return &gomemcached.MCResponse{
				Status: gomemcached.EINVAL,
				Body:   []byte(fmt.Sprintf("unsupported SASL auth mech: %v", req.Key)),
			}
		}
		targetUserPswd := bytes.Split(req.Body, []byte("\x00"))
		if len(targetUserPswd) != 3 {
			return &gomemcached.MCResponse{
				Status: gomemcached.EINVAL,
				Body:   []byte("invalid SASL auth body"),
			}
		}
		targetBucket := rh.buckets.Get(string(targetUserPswd[1]))
		if targetBucket == nil {
			return &gomemcached.MCResponse{
				Status: gomemcached.EINVAL,
				Body:   []byte("not a bucket"),
			}
		}
		if !targetBucket.Auth(targetUserPswd[2]) {
			return &gomemcached.MCResponse{
				Status: gomemcached.EINVAL,
				Body:   []byte("failed auth"),
			}
		}
		rh.currentBucket = targetBucket
		return &gomemcached.MCResponse{}
	case gomemcached.TAP_CONNECT:
		chpkt, cherr := transmitPackets(w)
		err := doTap(rh.currentBucket, req, chpkt, cherr)
		// Currently no way for this to return without failing
		log.Printf("Error tapping: %v", err)
		return &gomemcached.MCResponse{Fatal: true}
	case gomemcached.STAT:
		err := doStats(rh.currentBucket, w, string(req.Key))
		if err != nil {
			log.Printf("Error sending stats: %v", err)
			return &gomemcached.MCResponse{Fatal: true}
		}
		return nil
	}

	vb := rh.currentBucket.GetVBucket(req.VBucket)
	if vb == nil {
		return &gomemcached.MCResponse{
			Status: gomemcached.NOT_MY_VBUCKET,
		}
	}

	return vb.Dispatch(w, req)
}

func sessionLoop(s io.ReadWriteCloser, addr string, handler *reqHandler) {
	log.Printf("Started session with %v", addr)
	defer func() {
		log.Printf("Finished session with %v", addr)
	}()
	memcached.HandleIO(s, handler)
}

func waitForConnections(ls net.Listener, buckets *Buckets, defaultBucketName string) {
	for {
		s, e := ls.Accept()
		if e == nil {
			log.Printf("Got a connection from %v", s.RemoteAddr())
			bucket := buckets.Get(defaultBucketName)
			handler := &reqHandler{
				buckets:       buckets,
				currentBucket: bucket,
			}
			go sessionLoop(s, s.RemoteAddr().String(), handler)
		} else {
			log.Printf("Error accepting from %s: %v", ls, e)
			// TODO:  Figure out if this is recoverable.
			// It probably is most of the time, but not during tests.
			return
		}
	}
}

func StartServer(addr string, buckets *Buckets, defaultBucketName string) (net.Listener, error) {
	ls, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}

	go waitForConnections(ls, buckets, defaultBucketName)
	return ls, nil
}

func GetVBucketForKey(b Bucket, key []byte) *vbucket {
	return b.GetVBucket(VBucketIdForKey(key, b.GetBucketSettings().NumPartitions))
}

func GetVBucket(b Bucket, key []byte, vbs VBState) *vbucket {
	vb := GetVBucketForKey(b, key)
	if vb == nil || vb.GetVBState() != vbs {
		return nil
	}
	return vb
}

func GetItem(b Bucket, key []byte, vbs VBState) *gomemcached.MCResponse {
	vb := GetVBucket(b, key, vbs)
	if vb == nil {
		return nil
	}
	// TODO: Possible race here with concurrent change to vbstate?
	return vbGet(vb, nil, &gomemcached.MCRequest{
		Opcode:  gomemcached.GET,
		VBucket: vb.vbid,
		Key:     key,
	})
}

func SetItem(b Bucket, key []byte, val []byte, vbs VBState) *gomemcached.MCResponse {
	vb := GetVBucket(b, key, vbs)
	if vb == nil {
		return nil
	}
	return vbMutate(vb, nil, &gomemcached.MCRequest{
		Opcode:  gomemcached.SET,
		VBucket: vb.vbid,
		Key:     key,
		Body:    val,
	})
}
