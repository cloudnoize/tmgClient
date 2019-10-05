package main

import (
	"log"
	"os"
	"strconv"
	"time"

	"github.com/cloudnoize/conv"
	locklessq "github.com/cloudnoize/locklessQ"
	"github.com/cloudnoize/wavreader"
)

func saveWav(q *locklessq.Qint16, sr uint32, filePath string) {
	var wh wavreader.WavHHeader
	var buff [44]byte
	//CHunkID
	copy(buff[:], []byte("RIFF"))
	//ChunkSize
	audioBytes := len(q.Q) * 2           //16bits e.g. 2 bytes
	chunksize := uint32(38 + audioBytes) //38 for rest of header
	conv.UInt32ToBytes(chunksize, buff[:], 4)
	//Format
	copy(buff[8:], []byte("WAVE"))
	//Subchunk1ID
	copy(buff[12:], []byte("fmt "))
	//Subchunk1Size
	conv.UInt32ToBytes(16, buff[:], 16)
	//Audioformat
	buff[20] = 1
	//NumChannels
	buff[22] = 1
	//Samplerate
	conv.UInt32ToBytes(sr, buff[:], 24)
	//Byterate
	conv.UInt32ToBytes(sr*1*16/8, buff[:], 28)
	//BlockAlign
	ba := byte(1 * 16 / 8)
	buff[32] = ba
	//BitsPerSample
	buff[34] = 16
	//Subchunk2ID
	copy(buff[36:], []byte("data"))
	//Subchunk2Size
	conv.UInt32ToBytes(uint32(audioBytes), buff[:], 40)

	wh.Hdr = append(wh.Hdr, buff[:]...)
	wh.String()

	filePath += "/" + GetFileName()

	// If the file doesn't exist, create it, or append to the file
	log.Println("Save to ", filePath)
	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		log.Fatal(err)
	}
	if _, err := f.Write(buff[:]); err != nil {
		log.Fatal(err)
	}
	//Write data
	bytebuff := make([]byte, audioBytes, audioBytes)
	println(audioBytes, len(q.Q))
	for i, v := range q.Q {
		conv.Int16ToBytes(v, bytebuff, i*2)
	}
	if _, err := f.Write(bytebuff); err != nil {
		log.Fatal(err)
	}
	if err := f.Close(); err != nil {
		log.Fatal(err)
	}
}

func GetFileName() string {
	return "generated_" + strconv.Itoa(int(time.Now().Unix())) + ".wav"
}
