// Copyright (c) 2018-2022 Evan Oberholster. All rights reserved.
// Use of this source code is governed by a license that can be
// found in the LICENSE file.

// Package jpeg reads metadata information (Exif and XMP) from a JPEG Image.
package jpeg

import (
	"bufio"
	"encoding/binary"
	"errors"
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

	// SOF Header and Tiff Header
	sofHeader

	// Reader
	pos       uint8
	discarded uint32
	br        *bufio.Reader
}

func newJPEGReader(r io.Reader, exifReader func(r io.Reader, header meta.ExifHeader) error, xmpReader func(r io.Reader) error) *jpegReader {
	br, ok := r.(*bufio.Reader)

	if !ok || br.Size() >= bufferSize {
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
		if buf, err = jr.br.Peek(16); err != nil {
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
				// Ignore DQT Markers
				return jr.ignoreMarker(buf)
			case markerDRI:
				return jr.discard(6)
			case markerAPP0:
				return jr.ignoreMarker(buf)
			case markerAPP2:
				if isICCProfilePrefix(buf) {
					// Ignore ICC Profile Marker
					return jr.ignoreMarker(buf)
				}
				return jr.ignoreMarker(buf)
			case markerAPP7, markerAPP8,
				markerAPP9, markerAPP10:
				return jr.ignoreMarker(buf)
			case markerAPP13:
				if isPhotoshopPrefix(buf) {
					// Ignore Photoshop Profile Marker
					return jr.ignoreMarker(buf)
				}
				return jr.ignoreMarker(buf)
			case markerAPP14:
				return jr.ignoreMarker(buf)
			case markerAPP1:
				return jr.readAPP1(buf)
			}
			if err != nil {
				return err
			}
		}
		break
	}
	return
}

func (jr *jpegReader) scanMarkers(buf []byte) (err error) {
	switch buf[1] {
	case markerSOF0, markerSOF1,
		markerSOF2, markerSOF3,
		markerSOF5, markerSOF6,
		markerSOF7, markerSOF9,
		markerSOF10:
		return jr.readSOF(buf)
	case markerDHT:
		// Artificial End Of Image for DHT Marker.
		// This is done to improve performance.
		if jr.pos == 1 {
			return ErrEndOfImage
		}
		// Ignore DHT Markers
		return jr.ignoreMarker(buf)
	case markerSOI:
		jr.pos++
		return jr.discard(2)
	case markerEOI:
		jr.pos--
		// Return EndOfImage
		if jr.pos == 1 {
			return ErrEndOfImage
		}
		return jr.discard(2)
	case markerDQT:
		// Ignore DQT Markers
		return jr.ignoreMarker(buf)
	case markerDRI:
		return jr.discard(6)
	case markerAPP0:
		return jr.ignoreMarker(buf)
	case markerAPP2:
		if isICCProfilePrefix(buf) {
			// Ignore ICC Profile Marker
			return jr.ignoreMarker(buf)
		}
		return jr.ignoreMarker(buf)
	case markerAPP7, markerAPP8,
		markerAPP9, markerAPP10:
		return jr.ignoreMarker(buf)
	case markerAPP13:
		if isPhotoshopPrefix(buf) {
			// Ignore Photoshop Profile Marker
			return jr.ignoreMarker(buf)
		}
		return jr.ignoreMarker(buf)
	case markerAPP14:
		return jr.ignoreMarker(buf)
	case markerAPP1:
		return jr.readAPP1(buf)
	}
	return jr.discard(1)
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
	// APP1 XML Marker
	if isXMPPrefix(buf) {
		return jr.readXMP(buf)
	}
	// APP1 Exif Marker
	if isJpegExifPrefix(buf) {
		return jr.readExif(buf)
	}
	return nil
}

// readExif reads the Exif header/component with the addtached metadata
// ExifDecodeFn. If the function is nil it discards the exif length.
func (jr *jpegReader) readExif(buf []byte) (err error) {
	// Read the length of the Exif Information
	remain := int(jpegByteOrder.Uint16(buf[2:4]) - exifPrefixLength)

	// Discard App Marker bytes and Exif header bytes
	if err = jr.discard(2 + exifPrefixLength); err != nil {
		return err
	}

	// Peek at TiffHeader information
	if buf, err = jr.br.Peek(exifPrefixLength); err != nil {
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
func (m *jpegReader) readXMP(buf []byte) (err error) {
	// Read the length of the XMPHeader
	remain := int(jpegByteOrder.Uint16(buf[2:4])) - 2 - xmpPrefixLength

	// Discard App Marker bytes and header length bytes
	if err = m.discard(4 + xmpPrefixLength); err != nil {
		return err
	}
	//xmpHeader := meta.NewXMPHeader(m.discarded, uint32(remain))

	// Read XMP Decode Function here
	if m.XMPReader != nil {
		r := io.LimitReader(m.br, int64(remain))
		if err = m.XMPReader(r); err != nil {
			return err
		}
		// Discard remaining bytes
		remain = int(r.(*io.LimitedReader).N)
	}

	// Discard remaining bytes
	return m.discard(remain)
}

// readSOF reads a JPEG Start of file with the uint16
// width, height, and components of the JPEG image.
func (m *jpegReader) readSOF(buf []byte) error {
	length := int(jpegByteOrder.Uint16(buf[2:4]))
	height := jpegByteOrder.Uint16(buf[5:7])
	width := jpegByteOrder.Uint16(buf[7:9])
	comp := uint8(buf[9])
	header := sofHeader{height, width, comp}
	if m.pos == 1 {
		m.sofHeader = header
	}
	return m.discard(length + 2)
}

// ignoreMarker reads the Marker Header length and then
// discards the said marker and its header length
func (m *jpegReader) ignoreMarker(buf []byte) error {
	// Read Marker Header Length
	length := int(jpegByteOrder.Uint16(buf[2:4]))

	// Discard Marker Header Length and Marker Length
	return m.discard(length + 2)
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
	markerDHT = 0xC4
	markerSOI = 0xD8
	markerEOI = 0xD9
	markerDQT = 0xDB
	markerDRI = 0xDD

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

// Prefix lengths
const (
	xmpPrefixLength  = 29
	exifPrefixLength = 8
)

// jpegByteOrder JPEG always uses a BigEndian byteorder inside the JPEG image.
// Can use either byteorder for Exif Information inside the JPEG image.
var jpegByteOrder = binary.BigEndian

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

// PhotoshopPrefix returns true if
// buf[4:14] equals "Photoshop 3.0\000",
// buf[0:2] is AppMarker, buf[2:4] is HeaderLength
func isPhotoshopPrefix(buf []byte) bool {
	return buf[4] == 0x50 &&
		buf[5] == 0x68 &&
		buf[6] == 0x6f &&
		buf[7] == 0x74 &&
		buf[8] == 0x6f &&
		buf[9] == 0x70 &&
		buf[10] == 0x20 &&
		buf[11] == 0x33 &&
		buf[12] == 0x2e &&
		buf[13] == 0x30 &&
		buf[14] == 0x00
}

// isICCProfilePrefix returns true if
// buf[4:14] equals []byte,
// buf[0:2] is AppMarker, buf[2:4] is HeaderLength
func isICCProfilePrefix(buf []byte) bool {
	return buf[4] == 0x49 &&
		buf[5] == 0x43 &&
		buf[6] == 0x43 &&
		buf[7] == 0x5f &&
		buf[8] == 0x50 &&
		buf[9] == 0x52 &&
		buf[10] == 0x4f &&
		buf[11] == 0x46 &&
		buf[12] == 0x49 &&
		buf[13] == 0x4c &&
		buf[14] == 0x45
}

// isXMPPrefix returns true if
// buf[4:15] equals "http://ns.adobe.com/xap/1.0/\000",
// buf[0:2] is AppMarker, buf[2:4] is HeaderLength
func isXMPPrefix(buf []byte) bool {
	return buf[4] == 0x68 &&
		buf[5] == 0x74 &&
		buf[6] == 0x74 &&
		buf[7] == 0x70 &&
		buf[8] == 0x3a &&
		buf[9] == 0x2f &&
		buf[10] == 0x2f &&
		buf[11] == 0x6e &&
		buf[12] == 0x73 &&
		buf[13] == 0x2e &&
		buf[14] == 0x61 &&
		buf[15] == 0x64
}

// isJpegExifPrefix returns true if
// buf[4:9] equals "Exif" and '0', '0',
// buf[0:2] is AppMarker, buf[2:4] is HeaderLength
func isJpegExifPrefix(buf []byte) bool {
	return buf[4] == 0x45 &&
		buf[5] == 0x78 &&
		buf[6] == 0x69 &&
		buf[7] == 0x66 &&
		buf[8] == 0x00 &&
		buf[9] == 0x00
}
