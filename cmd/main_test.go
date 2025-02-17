package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"testing"

	"github.com/evanoberholster/imagemeta"
	"github.com/evanoberholster/imagemeta/xmp"
)

func BenchmarkExif(b *testing.B) {
	f, err := os.Open("../../test/img/1.CR3")
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		err = f.Close()
		if err != nil {
			panic(err)
		}
	}()

	buf, _ := ioutil.ReadAll(f)
	cb := bytes.NewReader(buf)
	b.ResetTimer()
	b.ReportAllocs()

	var x xmp.XMP
	//var e *exif.Data
	//exifFn := func(r meta.Reader, h meta.ExifHeader) error {
	//	e, err = exif.ParseExif(r, h)
	//	return nil
	//}

	for i := 0; i < b.N; i++ {
		cb.Seek(0, 0)
		//err := tiff.Scan(cb, imagetype.ImageCR3, exifFn, nil)
		m, err := imagemeta.Parse(cb)
		if err != nil {
			fmt.Println(err)
		}
		e, err := m.Exif()
		if err != nil {
			panic(err)
		}
		_ = x
		if e != nil {

			_, _ = e.Artist()
			_, _ = e.Copyright()
			_ = e.CameraMake()
			_ = e.CameraModel()
			_, _ = e.CameraSerial()
			_ = e.Orientation()
			_, _ = e.LensMake()
			_, _ = e.LensModel()
			_, _ = e.LensSerial()
			_, _ = e.ISOSpeed()
			_, _ = e.FocalLength()
			_, _ = e.LensModel()
			_, _ = e.Aperture()
			_, _ = e.ShutterSpeed()
			//_, _ = e.ExposureValue()
			_, _ = e.ExposureBias()

			//_, _, _ = e.GPSCoords()
			//c, _ := e.GPSCellID()
			//_ = c.ToToken()
			//_, _ = e.DateTime(time.Local)
			//_, _ = e.ModifyDate(time.Local)

			//fmt.Println(e.GPSDate(nil))
		}
	}

}
