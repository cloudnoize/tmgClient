package main

import (
	"bufio"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
	"unsafe"

	"github.com/cloudnoize/conv"
	"github.com/cloudnoize/elport"
	locklessq "github.com/cloudnoize/locklessQ"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	validSamples = promauto.NewCounter(prometheus.CounterOpts{
		Name: "valid_samples",
		Help: "The total number of valid samples",
	})
	badSamples = promauto.NewCounter(prometheus.CounterOpts{
		Name: "bad_samples",
		Help: "The total number of bad samples",
	})
	pauseSamples = promauto.NewCounter(prometheus.CounterOpts{
		Name: "pause_samples",
		Help: "The total number of pauses",
	})
	buuferPrev = promauto.NewCounter(prometheus.CounterOpts{
		Name: "buffer_prev",
		Help: "USe previous buffer",
	})
)

type streamImp struct {
	q32            *locklessq.Qfloat32
	q16            *locklessq.Qint16
	cb16           *CyclicBuffer16bit
	seqOfBads      uint
	bitrate        int
	frames         int
	test           bool
	count          int
	measure        int
	toErr          bool
	start          time.Time
	pause          time.Time
	lastGoodSample int16
}

func (s *streamImp) CallBack(inputBuffer, outputBuffer unsafe.Pointer, frames uint64) {
	if s.bitrate == 16 {
		s.out16bit(outputBuffer, frames)
		return
	}
	s.out32bit(outputBuffer, frames)
}

func (s *streamImp) out32bit(outputBuffer unsafe.Pointer, frames uint64) {
	ob := (*[1024]float32)(outputBuffer)
	errNum := 0
	for i := 0; i < s.frames; i++ {
		val, ok := s.q32.Pop()
		if ok {
			if !s.test {
				(*ob)[i] = val
			} else {
				(*ob)[i] = 0
				if val != 1 {
					errNum++
					println("recieved ", val)
				}
			}

		} else {
			errNum++
		}
	}
	if errNum != 0 {
		if s.measure == 0 {
			s.start = time.Now()
		}
		s.measure++
	} else {
		if s.measure != 0 {
			ms := float64(time.Since(s.start) / time.Millisecond)
			fmt.Println("Time since last good packet ", ms, "ms frames elapsed ", s.measure)
			s.measure = 0
		}
	}
	println("frame ", s.count, " got ", errNum, " erred vals")

	s.count++
}

func (s *streamImp) out16bit(outputBuffer unsafe.Pointer, frames uint64) {
	ob := (*[1024]int16)(outputBuffer)
	if s.test {
		for i := 0; i < s.frames; i++ {
			val, ok := s.q16.Pop()
			(*ob)[i] = 0
			if !ok {
				pauseSamples.Inc()
				continue
			}
			if val != 128 {
				badSamples.Inc()
				continue
			}
			validSamples.Inc()
		}
		return
	}
	//If pop is not ok, place silence for $duration and resume.
	//Hopefully samples have acuumulated.
	for i := 0; i < s.frames; i++ {
		if time.Since(s.pause) < time.Duration(700)*time.Millisecond {
			pauseSamples.Inc()
			(*ob)[i] = 0
			continue
		}
		val, ok := s.q16.Pop()
		if ok {
			(*ob)[i] = int16(val)
			validSamples.Inc()
			s.cb16.push(int16(val))
			s.lastGoodSample = int16(val)
		} else {
			s.pause = time.Now()
			badSamples.Inc()
			s.lastGoodSample = (s.cb16.pop() + s.lastGoodSample) / 2
			(*ob)[i] = s.lastGoodSample
		}
	}
}

func (s *streamImp) Write(b []byte) (n int, err error) {
	if s.toErr {
		return 0, errors.New("Generated err")
	}
	if s.bitrate == 16 {
		s.Write16int(b)
		return len(b), nil
	}
	s.Write32float(b)
	return len(b), nil
}

func (this *streamImp) Write32float(b []byte) {
	for i := 0; i < len(b)/4; i++ {
		f := conv.BytesToFloat32(b, i*4)
		this.q32.Insert(f)
	}
}

func (this *streamImp) Write16int(b []byte) {
	for i := 0; i < len(b)/2; i++ {
		s := conv.BytesToint16(b, i*2)
		this.q16.Insert(s)
	}
}

func main() {

	go func() {
		http.Handle("/metrics", promhttp.Handler())
		http.ListenAndServe(":2112", nil)
	}()

	bitrate := int(32)
	if v := os.Getenv("BIT_RATE"); v != "" {
		brate, _ := strconv.Atoi(v)
		switch brate {
		case 16:
			bitrate = brate
		}
	}

	frames := 512
	if v := os.Getenv("FRAMES"); v != "" {
		frames, _ = strconv.Atoi(v)
	}
	println("frames - ", frames)
	sigs := make(chan os.Signal, 1)
	done := make(chan bool, 1)

	addr := os.Getenv("ADDR")

	test := false
	if v := os.Getenv("TEST"); v != "" {
		test = true
		println("test mode")
	}

	udpaddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		println(err.Error())
		return
	}
	conn, err := net.DialUDP("udp", nil, udpaddr)
	if err != nil {
		println(err.Error())
		return
	}

	sr := 48000
	if v := os.Getenv("SR"); v != "" {
		sr, _ = strconv.Atoi(v)
	}
	cybsize := 256
	if v := os.Getenv("CYBSIZE"); v != "" {
		cybsize, _ = strconv.Atoi(v)
	}

	si := &streamImp{q32: locklessq.NewQfloat32(int32(sr * 1)), q16: locklessq.NewQint16(int32(sr * 300)), bitrate: bitrate, frames: frames, test: test, cb16: New16BitCyclicBuffer(uint(cybsize))}

	pa.CbStream = si

	pa.Initialize()
	sf := pa.Float32

	if bitrate == 16 {
		sf = pa.Int16
	}

	s, _ := pa.OpenDefaultStream(0, 1, sf, float64(sr), uint64(frames), nil)

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Enter signal: ")
	text, _ := reader.ReadString('\n')
	println("sending ", text)
	conn.Write([]byte(text))
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		var buf [1024]byte
		n, err := conn.Read(buf[:])

		if err != nil {
			log.Fatal(err)
		}
		_, err = si.Write(buf[:n])
		if err != nil {
			log.Fatal(err)
		}
		s.Start()
		go func() {
			sig := <-sigs
			fmt.Println()
			fmt.Println(sig)
			si.toErr = true
			s.Stop()
			s.Close()
			pa.Terminate()
			conn.Close()
		}()
		// io.Copy(si, conn)
		for {
			n, err := conn.Read(buf[:])

			if err != nil {
				break
			}
			_, err = si.Write(buf[:n])
			if err != nil {
				break
			}
		}
		done <- true
	}()

	<-done

	if v := os.Getenv("SAVE"); v != "" {
		log.Println("saving to file")
		saveWav(si.q16, uint32(sr), "/Users/elerer/WAV_FILES")
	}

	fmt.Println("exiting")

}
