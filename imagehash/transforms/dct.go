// Package transforms provides the transformations for imagehash
package transforms

// Copyright 2017 The goimagehash Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

import (
	"math"
	"sync"
)

// DCT1D function returns result of DCT-II.
// DCT type II, unscaled. Algorithm by Byeong Gi Lee, 1984.
func DCT1D(input []float64) []float64 {
	temp := make([]float64, len(input))
	forwardTransform(input, temp, len(input))
	return input
}

func forwardTransform(input, temp []float64, Len int) {
	if Len == 1 {
		return
	}

	halfLen := Len / 2

	for i := 0; i < halfLen; i++ {
		x, y := input[i], input[Len-1-i]
		temp[i] = x + y
		temp[i+halfLen] = (x - y) / (math.Cos((float64(i)+0.5)*math.Pi/float64(Len)) * 2)
	}
	forwardTransform(temp, input, halfLen)
	forwardTransform(temp[halfLen:], input, halfLen)
	for i := 0; i < halfLen-1; i++ {
		input[i*2+0] = temp[i]
		input[i*2+1] = temp[i+halfLen] + temp[i+halfLen+1]
	}

	input[Len-2], input[Len-1] = temp[halfLen-1], temp[Len-1]
}

// DCT2D function returns a  result of DCT2D by using the seperable property.
func DCT2D(input [][]float64, w int, h int) [][]float64 {
	output := make([][]float64, h)
	for i := range output {
		output[i] = make([]float64, w)
	}

	wg := new(sync.WaitGroup)
	for i := 0; i < h; i++ {
		wg.Add(1)
		go func(i int) {
			output[i] = DCT1D(input[i])
			wg.Done()
		}(i)
	}

	wg.Wait()
	for i := 0; i < w; i++ {
		wg.Add(1)
		in := make([]float64, h)
		go func(i int) {
			for j := 0; j < h; j++ {
				in[j] = output[j][i]
			}
			rows := DCT1D(in)
			for j := 0; j < len(rows); j++ {
				output[j][i] = rows[j]
			}
			wg.Done()
		}(i)
	}

	wg.Wait()
	return output
}

// DCT2DFast function returns a result of DCT2D by using the seperable property.
// DCT type II, unscaled. Algorithm by Byeong Gi Lee, 1984.
// Fast version only works with pHashSize 64 will panic if another since is given.
func DCT2DFast(input *[]float64) {
	if len(*input) != 4096 {
		panic("Incorrect forward transform size")
	}
	for i := 0; i < 64; i++ { // height
		forwardDCT64((*input)[i*64 : 64*i+64])
	}

	var row [64]float64
	for i := 0; i < 64; i++ { // width
		for j := 0; j < 64; j++ {
			row[j] = (*input)[64*j+i]
		}
		forwardDCT64(row[:])
		for j := 0; j < 64; j++ {
			(*input)[64*j+i] = row[j]
		}
	}
}

// DCT2DHash64 function returns a result of DCT2D by using the seperable property.
// DCT type II, unscaled. Algorithm by Byeong Gi Lee, 1984.
// Cusstom built for Hash64. Returns flattened pixels
func DCT2DHash64(input *[]float64) [64]float64 {
	var flattens [64]float64
	if len(*input) != 64*64 {
		panic("Incorrect forward transform size")
	}
	for i := 0; i < 64; i++ { // height
		forwardDCT64((*input)[i*64 : 64*i+64])
	}

	var row [64]float64
	for i := 0; i < 8; i++ { // width
		for j := 0; j < 64; j++ {
			row[j] = (*input)[64*j+i]
		}
		forwardDCT64(row[:])
		for j := 0; j < 8; j++ {
			flattens[8*j+i] = row[j]
		}
	}
	return flattens
}

// DCT2DHash256 function returns a result of DCT2D by using the seperable property.
// DCT type II, unscaled. Algorithm by Byeong Gi Lee, 1984.
// Cusstom built for Hash256. Returns flattened pixels
func DCT2DHash256(input *[]float64) [256]float64 {
	var flattens [256]float64
	if len(*input) != 256*256 {
		panic("Incorrect forward transform size")
	}
	for i := 0; i < 256; i++ { // height
		forwardDCT256((*input)[i*256 : 256*i+256])
	}

	var row [4096]float64
	for i := 0; i < 16; i++ { // width
		for j := 0; j < 256; j++ {
			row[j] = (*input)[256*j+i]
		}
		forwardDCT256(row[:])
		for j := 0; j < 16; j++ {
			flattens[16*j+i] = row[j]
		}
	}
	return flattens
}
