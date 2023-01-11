// Copyright (c) 2018-2023 Evan Oberholster. All rights reserved.
// Use of this source code is governed by a license that can be
// found in the LICENSE file.

// Package jpeg reads metadata information (Exif and XMP) from a JPEG Image.
package jpeg

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/evanoberholster/imagemeta/imagetype"
	"github.com/evanoberholster/imagemeta/meta"
)

// Errors
var (
	ErrNoExif       = meta.ErrNoExif
	ErrNoJPEGMarker = errors.New("no JPEG Marker")
	ErrEndOfImage   = errors.New("end of Image")
)

const (
	bufferSize int = 4 * 1024 // 4Kb
)

type jpegReader struct {
	ExifReader func(r io.Reader, h meta.ExifHeader) error
	XMPReader  func(r io.Reader) error

	// Reader
	br *bufio.Reader

	// SOF Header
	sofHeader

	// Reader
	pos       uint8
	discarded uint32
}

func newJPEGReader(r io.Reader, exifReader func(r io.Reader, header meta.ExifHeader) error, xmpReader func(r io.Reader) error) *jpegReader {
	br, ok := r.(*bufio.Reader)

	if !ok || br.Size() <= bufferSize {
		br = bufio.NewReaderSize(r, bufferSize)
	}

	return &jpegReader{br: br, ExifReader: exifReader, XMPReader: xmpReader}
}

// ScanJPEG scans a reader for JPEG Image markers. exifReader and xmpReader are run at their respective
// positions during the scan. Returns en error.
//
// Returns the error ErrNoJPEGMarker if a JPEG SOF was not found.
func ScanJPEG(r io.Reader, exifReader func(r io.Reader, header meta.ExifHeader) error, xmpReader func(r io.Reader) error) (err error) {
	defer func() {
		if state := recover(); state != nil {
			err = state.(error)
		}
	}()
	jr := newJPEGReader(r, exifReader, xmpReader)

	var buf []byte
	for {
		if buf, err = jr.peek(16); err != nil {
			err = ErrNoJPEGMarker
			return
		}

		if !isMarkerFirstByte(buf) {
			_ = jr.discard(1)
			continue
		}
		if isSOIMarker(buf) {
			jr.pos++
			_ = jr.discard(2)
			continue
		}
		if jr.pos > 0 {
			if logInfo() {
				logInfoMarker(markerString(buf[1]), markerLength(buf), int(jr.discarded))
			}
			switch buf[1] {
			case markerSOF0, markerSOF1,
				markerSOF2, markerSOF3,
				markerSOF5, markerSOF6,
				markerSOF7, markerSOF9,
				markerSOF10:
				err = jr.readSOF(buf)
			case markerDHT:
				// Artificial End Of Image for DHT Marker.
				// This is done to improve performance.
				if jr.pos == 1 {
					return ErrEndOfImage
				}
				// Ignore DHT Markers
				err = jr.ignoreMarker(buf)
			case markerSOI:
				jr.pos++
				err = jr.discard(2)
			case markerEOI:
				jr.pos--
				// Return EndOfImage
				if jr.pos == 1 {
					return ErrEndOfImage
				}
				err = jr.discard(2)
			case markerDQT:
				// Ignore DQT Markers and close parsing
				err = jr.ignoreMarker(buf)
				continue
				//return nil
			case markerDRI:
				return jr.discard(6)
			case markerAPP0:
				// Is JFIF Marker
				if isJFIFPrefix(buf) || isJFIFPrefixExt(buf) {
					l := jfifHeader(buf)
					if logInfo() {
						logInfoMarker("APP0 JFIF", l, int(jr.discarded))
					}
					err = jr.discard(l + 2)
				} else {
					err = jr.ignoreMarker(buf)
				}
				continue
			case markerAPP1:
				err = jr.readAPP1(buf)
				continue
			case markerAPP2:
				if isICCProfilePrefix(buf) {
					if logInfo() {
						logInfoMarker("APP2 ICC Profile", markerLength(buf), int(jr.discarded))
					}
					// Ignore ICC Profile Marker
					err = jr.ignoreMarker(buf)
					continue
				}
				err = jr.ignoreMarker(buf)
				continue
			case markerAPP7, markerAPP8,
				markerAPP9, markerAPP10:
				return jr.ignoreMarker(buf)
			case markerAPP13:
				if isPhotoshopPrefix(buf) {
					// Ignore Photoshop Profile Marker
					err = jr.ignoreMarker(buf)
					continue
				}
				err = jr.ignoreMarker(buf)
				continue
			case markerAPP14:
				return jr.ignoreMarker(buf)
			}
			if err != nil {
				return err
			}
		}
		break
	}
	return
}

// peek returns the next n bytes without advancing the unerlying bufio.Reader
func (jr *jpegReader) peek(n int) ([]byte, error) {
	return jr.br.Peek(n)
}

// discard adds to m.discarded and discards from the underlying bufio.Reader
func (jr *jpegReader) discard(i int) (err error) {
	if i == 0 {
		return
	}
	i, err = jr.br.Discard(i)
	jr.discarded += uint32(i)
	return
}

// readAPP1
func (jr *jpegReader) readAPP1(buf []byte) (err error) {
	// APP1 Exif Marker
	if isExifPrefix(buf) {
		if logInfo() {
			logInfoMarker("APP1 Exif", markerLength(buf), int(jr.discarded))
		}
		return jr.readExif(buf)
	}

	// APP1 XMP Marker
	if isXMPPrefix(buf) {
		if logInfo() {
			logInfoMarker("APP1 XMP", markerLength(buf), int(jr.discarded))
		}
		return jr.readXMP(buf)
	}

	// APP1 XMP Extension marker (NOT SUPPORTED)
	if isXMPPrefixExt(buf) {
		if logInfo() {
			logInfoMarker("APP1 XMP Extension", markerLength(buf), int(jr.discarded))
		}
		return jr.ignoreMarker(buf)
	}

	return jr.ignoreMarker(buf)
}

// readExif reads the Exif header/component with the addtached metadata
// ExifDecodeFn. If the function is nil it discards the exif length.
func (jr *jpegReader) readExif(buf []byte) (err error) {
	// Read the length of the Exif Information
	remain := markerLength(buf) - exifPrefixLength

	// Discard App Marker bytes and Exif header bytes
	if err = jr.discard(2 + exifPrefixLength); err != nil {
		return err
	}

	// Peek at TiffHeader information
	if buf, err = jr.peek(exifPrefixLength); err != nil {
		return err
	}

	// Read Exif
	if jr.ExifReader != nil {
		// Create a TiffHeader from the Tiff directory ByteOrder, root IFD Offset,
		// the tiff Header Offset, and the length of the exif information.
		byteOrder := meta.BinaryOrder(buf)
		firstIfdOffset := byteOrder.Uint32(buf[4:8])
		exifLength := uint32(remain)

		// Set Tiff Header
		exifHeader := meta.NewExifHeader(byteOrder, firstIfdOffset, jr.discarded, exifLength, imagetype.ImageJPEG)

		if err = jr.ExifReader(jr.br, exifHeader); err != nil {
			return err
		}
		// Discard remaining bytes
		remain = 0
	}

	// Discard remaining bytes
	return jr.discard(remain)
}

// readXMP reads the Exif header/component with the addtached metadata
// XmpDecodeFn. If the function is nil it discards the exif length.
func (jr *jpegReader) readXMP(buf []byte) (err error) {
	// Read the length of the XMPHeader
	remain := markerLength(buf) - 2 - xmpPrefixLength

	// Discard App Marker bytes and header length bytes
	if err = jr.discard(4 + xmpPrefixLength); err != nil {
		return err
	}
	// Read XMP Decode Function here
	if jr.XMPReader != nil {
		r := io.LimitReader(jr.br, int64(remain))
		if err = jr.XMPReader(r); err != nil {
			return err
		}
		// Discard remaining bytes
		remain = int(r.(*io.LimitedReader).N)
	}
	// Discard remaining bytes
	return jr.discard(remain)
}

// readSOF reads a JPEG Start of file with the uint16
// width, height, and components of the JPEG image.
func (jr *jpegReader) readSOF(buf []byte) error {
	length := markerLength(buf)
	height := jpegEndian.Uint16(buf[5:7])
	width := jpegEndian.Uint16(buf[7:9])
	comp := uint8(buf[9])
	header := sofHeader{height, width, comp}
	if jr.pos == 1 {
		jr.sofHeader = header
	}
	return jr.discard(length + 2)
}

// ignoreMarker reads the Marker Header length and then
// discards the said marker and its header length
func (jr *jpegReader) ignoreMarker(buf []byte) error {
	return jr.discard(markerLength(buf) + 2)
}

// markerLength reads Marker Header Length
func markerLength(buf []byte) int {
	return int(jpegEndian.Uint16(buf[2:4]))
}

// Markers refers to the second byte of a JPEG Marker.
// The first is always 0xFF
const (
	markerFirstByte = 0xFF

	// SOF Markers
	markerSOF0  = 0xC0
	markerSOF1  = 0xC1
	markerSOF2  = 0xC2
	markerSOF3  = 0xC3
	markerSOF5  = 0xC5
	markerSOF6  = 0xC6
	markerSOF7  = 0xC7
	markerSOF9  = 0xC9
	markerSOF10 = 0xCA
	markerSOF11 = 0xCB

	// Other Markers
	markerDHT       = 0xC4
	markerSOI       = 0xD8
	markerEOI       = 0xD9
	markerImageData = 0xD9
	markerDQT       = 0xDB
	markerDRI       = 0xDD

	// APP Markers
	markerAPP0  = 0xE0
	markerAPP1  = 0xE1
	markerAPP2  = 0xE2
	markerAPP7  = 0xE7
	markerAPP8  = 0xE8
	markerAPP9  = 0xE9
	markerAPP10 = 0xEA
	markerAPP13 = 0xED
	markerAPP14 = 0xEE
)

var (
	mapMarkerString = map[uint8]string{
		markerSOF0:  "SOF0",
		markerSOF1:  "SOF1",
		markerSOF2:  "SOF2",
		markerSOF3:  "SOF3",
		markerSOF5:  "SOF5",
		markerSOF6:  "SOF6",
		markerSOF7:  "SOF7",
		markerSOF9:  "SOF9",
		markerSOF10: "SOF10",
		markerSOF11: "SOF11",
		markerDHT:   "DHT",
		markerSOI:   "SOI",
		markerEOI:   "EOI",
		markerDQT:   "DQT",
		markerDRI:   "DRI",
		markerAPP0:  "APP0",
		markerAPP1:  "APP1",
		markerAPP2:  "APP2",
		markerAPP7:  "APP7",
		markerAPP8:  "APP8",
		markerAPP9:  "APP9",
		markerAPP10: "APP10",
		markerAPP13: "APP13",
		markerAPP14: "APP14",
	}
)

// markerString returns the string name of the second byte of a JPEG marker
func markerString(m byte) string {
	str, ok := mapMarkerString[uint8(m)]
	if ok {
		return str
	}
	return fmt.Sprintf("Unknown marker %x", uint8(m))
}

const (
	exifPrefix       = "Exif\000\000"
	jfifPrefix       = "JFIF\000"
	jfifPrefixExt    = "JFXX\000"
	iccPrefix        = "ICC_PROFILE"
	xmpPrefix        = "http://ns.adobe.com/xap/1.0/\000"
	xmpPrefixExt     = "http://ns.adobe.com/xmp/extension/"
	photoshopPrefix  = "Photoshop "
	exifPrefixLength = 8
	xmpPrefixLength  = 29
)

var (
	// jpegEndian JPEG and JFIF always use BigEndian byteorder.
	// Can use either byteorder for Exif Information inside the JPEG image.
	jpegEndian = binary.BigEndian
)

// sofHeader contains height, width and number of components.
type sofHeader struct {
	height     uint16
	width      uint16
	components uint8
}

// isSOIMarker returns true if the first 2 bytes match an SOI marker
func isSOIMarker(buf []byte) bool {
	return buf[0] == markerFirstByte &&
		buf[1] == markerSOI
}

func isMarkerFirstByte(buf []byte) bool {
	return buf[0] == markerFirstByte
}

// PhotoshopPrefix returns true if marker matches photoshopPrefix
// buf[4:14] equals "Photoshop 3.0\000",
func isPhotoshopPrefix(buf []byte) bool {
	return string(buf[4:14]) == photoshopPrefix
}

// isICCProfilePrefix returns true if marker matches iccPrefix
func isICCProfilePrefix(buf []byte) bool {
	return string(buf[4:15]) == iccPrefix
}

// isXMPPrefix returns true if marker matches xmpPrefix
func isXMPPrefix(buf []byte) bool {
	return string(buf[4:33]) == xmpPrefix
}

// isXMPPrefixExt returns true if marker matches xmpPrefixExt
func isXMPPrefixExt(buf []byte) bool {
	return string(buf[4:38]) == xmpPrefixExt
}

// isJpegExifPrefix returns true if marker matches exifPrefix
func isExifPrefix(buf []byte) bool {
	return string(buf[4:10]) == exifPrefix
}

// isJFIFPrefix returns true if marker matches jfifPrefix
func isJFIFPrefix(buf []byte) bool {
	return string(buf[4:9]) == jfifPrefix
}

// isJFIFPrefixExt returns true of marker matches "JFXX"
func isJFIFPrefixExt(buf []byte) bool {
	return string(buf[4:9]) == jfifPrefixExt
}

// jfifHeader returns the lenth of JFIF header
func jfifHeader(buf []byte) int {
	if isJFIFPrefix(buf) {
		return markerLength(buf)
	}
	return 0
}
