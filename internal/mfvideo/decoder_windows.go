//go:build windows && amd64

package mfvideo

import (
	"context"
	"errors"
	"fmt"
	"image"
	"runtime"
	"sort"
	"time"
	"unsafe"

	"github.com/whiteboxsolutions/go-ole"
	"golang.org/x/sys/windows"
)

const (
	mfSourceReaderFirstVideoStream       = uint32(0xfffffffc)
	mfSourceReaderFlagError              = uint32(0x00000001)
	mfSourceReaderFlagEndOfStream        = uint32(0x00000002)
	mfSourceReaderFlagNativeTypeChanged  = uint32(0x00000010)
	mfSourceReaderFlagCurrentTypeChanged = uint32(0x00000020)
	decoderWidth                         = 1920
	decoderHeight                        = 1080
)

var (
	mfSourceReaderEnableVideoProcessing = guid{0xfb394f3d, 0xccf1, 0x42ee, [8]byte{0xbb, 0xb3, 0xf9, 0xb8, 0x45, 0xd5, 0x68, 0x1d}}
	mfMTDefaultStride                   = guid{0x644b4e48, 0x1e02, 0x4516, [8]byte{0xb0, 0xeb, 0xc0, 0x1c, 0xa9, 0xd4, 0x9a, 0xc6}}
	mfMTMinimumDisplayAperture          = guid{0xd7388766, 0x18fe, 0x48c6, [8]byte{0xa1, 0x77, 0xee, 0x89, 0x48, 0x67, 0xc8, 0xc4}}

	procMFCreateAttributes          = modMFPlat.NewProc("MFCreateAttributes")
	procMFCreateSourceReaderFromURL = modMFReadWrite.NewProc("MFCreateSourceReaderFromURL")
)

// Decoder serializes short, exact-timestamp thumbnail decoding operations.
// It never seeks or substitutes a nearby sample: each selected Evidence
// segment is decoded from its beginning and matched by Media Foundation time.
type Decoder struct {
	slot chan struct{}
}

type decodedFormat struct {
	width, height int
	stride        int
	cropX, cropY  int
	cropWidth     int
	cropHeight    int
}

type mfOffset struct {
	Fract uint16
	Value int16
}

type mfVideoArea struct {
	OffsetX mfOffset
	OffsetY mfOffset
	Width   int32
	Height  int32
}

func NewDecoder() *Decoder { return &Decoder{slot: make(chan struct{}, 1)} }

func (d *Decoder) Decode(ctx context.Context, path string, offsets []time.Duration, emit func(time.Duration, image.Image) error) error {
	if d == nil || ctx == nil || path == "" || emit == nil {
		return errors.New("Media Foundation decoder dependencies are required")
	}
	if len(offsets) == 0 {
		return errors.New("Media Foundation decoder offsets are required")
	}
	targets := append([]time.Duration(nil), offsets...)
	sort.Slice(targets, func(i, j int) bool { return targets[i] < targets[j] })
	for index, offset := range targets {
		if offset < 0 || offset != offset.Truncate(time.Second) {
			return fmt.Errorf("Media Foundation decoder offset %s is not a non-negative whole second", offset)
		}
		if index > 0 && offset == targets[index-1] {
			return fmt.Errorf("Media Foundation decoder offset %s is duplicated", offset)
		}
	}
	select {
	case d.slot <- struct{}{}:
		defer func() { <-d.slot }()
	case <-ctx.Done():
		return ctx.Err()
	}
	return decodeOnCurrentThread(ctx, path, targets, emit)
}

func decodeOnCurrentThread(ctx context.Context, path string, targets []time.Duration, emit func(time.Duration, image.Image) error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED); err != nil {
		return fmt.Errorf("initialize COM for Media Foundation decoder: %w", err)
	}
	defer ole.CoUninitialize()
	if err := callProc("MFStartup", procMFStartup, mfVersion, mfStartupFull); err != nil {
		return err
	}
	defer func() { _, _, _ = procMFShutdown.Call() }()

	reader, format, err := createSourceReader(path)
	if err != nil {
		return err
	}
	defer release(reader)

	targetIndex := 0
	for targetIndex < len(targets) {
		if err = ctx.Err(); err != nil {
			return err
		}
		var flags uint32
		var timestamp int64
		var sample unsafe.Pointer
		err = callMethod(
			"IMFSourceReader.ReadSample",
			reader,
			9,
			uintptr(mfSourceReaderFirstVideoStream),
			0,
			0,
			uintptr(unsafe.Pointer(&flags)),
			uintptr(unsafe.Pointer(&timestamp)),
			uintptr(unsafe.Pointer(&sample)),
		)
		if err != nil {
			return err
		}
		if flags&mfSourceReaderFlagError != 0 {
			release(sample)
			return errors.New("Media Foundation source reader reported a fatal stream error")
		}
		if flags&mfSourceReaderFlagNativeTypeChanged != 0 {
			release(sample)
			return errors.New("Media Foundation evidence video changed native media type during decode")
		}
		if flags&mfSourceReaderFlagCurrentTypeChanged != 0 {
			format, err = currentRGB32Format(reader)
			if err != nil {
				release(sample)
				return fmt.Errorf("validate changed Media Foundation output type: %w", err)
			}
		}
		if sample == nil {
			if flags&mfSourceReaderFlagEndOfStream != 0 {
				return fmt.Errorf("Media Foundation evidence video ended before offset %s", targets[targetIndex])
			}
			continue
		}
		targetTimestamp := int64(targets[targetIndex]/time.Second) * hundredNanosecondsPerSecond
		if timestamp < targetTimestamp {
			release(sample)
			continue
		}
		if timestamp > targetTimestamp {
			release(sample)
			return fmt.Errorf("Media Foundation evidence video skipped requested offset %s; next sample is %s", targets[targetIndex], time.Duration(timestamp/hundredNanosecondsPerSecond)*time.Second)
		}
		frame, frameErr := readRGB32Frame(sample, format)
		release(sample)
		if frameErr != nil {
			return frameErr
		}
		if err = emit(targets[targetIndex], frame); err != nil {
			return err
		}
		targetIndex++
	}
	return nil
}

func createSourceReader(path string) (unsafe.Pointer, decodedFormat, error) {
	wide, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, decodedFormat{}, err
	}
	var attributes unsafe.Pointer
	if err = callProc("MFCreateAttributes", procMFCreateAttributes, uintptr(unsafe.Pointer(&attributes)), 1); err != nil {
		return nil, decodedFormat{}, err
	}
	defer release(attributes)
	if err = setUINT32(attributes, "source reader video processing", &mfSourceReaderEnableVideoProcessing, 1); err != nil {
		return nil, decodedFormat{}, err
	}
	var reader unsafe.Pointer
	if err = callProc("MFCreateSourceReaderFromURL", procMFCreateSourceReaderFromURL, uintptr(unsafe.Pointer(wide)), uintptr(attributes), uintptr(unsafe.Pointer(&reader))); err != nil {
		return nil, decodedFormat{}, err
	}
	ok := false
	defer func() {
		if !ok {
			release(reader)
		}
	}()

	requested, err := createMediaType()
	if err != nil {
		return nil, decodedFormat{}, err
	}
	defer release(requested)
	if err = setGUID(requested, "decoder major type", &mfMTMajorType, &mfMediaTypeVideo); err != nil {
		return nil, decodedFormat{}, err
	}
	if err = setGUID(requested, "decoder subtype", &mfMTSubtype, &mfVideoFormatRGB32); err != nil {
		return nil, decodedFormat{}, err
	}
	if err = setUINT64(requested, "decoder frame size", &mfMTFrameSize, packPair(decoderWidth, decoderHeight)); err != nil {
		return nil, decodedFormat{}, err
	}
	if err = setUINT32(requested, "decoder default stride", &mfMTDefaultStride, decoderWidth*4); err != nil {
		return nil, decodedFormat{}, err
	}
	if err = callMethod("IMFSourceReader.SetCurrentMediaType", reader, 7, uintptr(mfSourceReaderFirstVideoStream), 0, uintptr(requested)); err != nil {
		return nil, decodedFormat{}, err
	}

	format, err := currentRGB32Format(reader)
	if err != nil {
		return nil, decodedFormat{}, err
	}
	ok = true
	return reader, format, nil
}

func currentRGB32Format(reader unsafe.Pointer) (decodedFormat, error) {
	var current unsafe.Pointer
	if err := callMethod("IMFSourceReader.GetCurrentMediaType", reader, 6, uintptr(mfSourceReaderFirstVideoStream), uintptr(unsafe.Pointer(&current))); err != nil {
		return decodedFormat{}, err
	}
	defer release(current)
	var packedSize uint64
	if err := callMethod("IMFAttributes.GetUINT64 frame size", current, 8, uintptr(unsafe.Pointer(&mfMTFrameSize)), uintptr(unsafe.Pointer(&packedSize))); err != nil {
		return decodedFormat{}, err
	}
	width, height := int(uint32(packedSize>>32)), int(uint32(packedSize))
	if width < decoderWidth || height < decoderHeight || width > 8192 || height > 8192 {
		return decodedFormat{}, fmt.Errorf("Media Foundation decoded storage frame %dx%d cannot contain 1920x1080 Evidence", width, height)
	}
	var subtype guid
	if err := callMethod("IMFAttributes.GetGUID subtype", current, 10, uintptr(unsafe.Pointer(&mfMTSubtype)), uintptr(unsafe.Pointer(&subtype))); err != nil {
		return decodedFormat{}, err
	}
	if subtype != mfVideoFormatRGB32 {
		return decodedFormat{}, errors.New("Media Foundation decoder output subtype is not RGB32")
	}
	var packedStride uint32
	if err := callMethod("IMFAttributes.GetUINT32 default stride", current, 7, uintptr(unsafe.Pointer(&mfMTDefaultStride)), uintptr(unsafe.Pointer(&packedStride))); err != nil {
		return decodedFormat{}, err
	}
	stride := int(int32(packedStride))
	if stride == 0 || absolute(stride) < width*4 {
		return decodedFormat{}, fmt.Errorf("Media Foundation decoded stride %d is invalid", stride)
	}
	format := decodedFormat{width: width, height: height, stride: stride, cropWidth: width, cropHeight: height}
	var area mfVideoArea
	var blobSize uint32
	apertureErr := callMethod(
		"IMFAttributes.GetBlob minimum display aperture",
		current,
		15,
		uintptr(unsafe.Pointer(&mfMTMinimumDisplayAperture)),
		uintptr(unsafe.Pointer(&area)),
		unsafe.Sizeof(area),
		uintptr(unsafe.Pointer(&blobSize)),
	)
	if apertureErr == nil {
		if blobSize != uint32(unsafe.Sizeof(area)) || area.OffsetX.Fract != 0 || area.OffsetY.Fract != 0 || area.OffsetX.Value < 0 || area.OffsetY.Value < 0 || area.Width != decoderWidth || area.Height != decoderHeight {
			return decodedFormat{}, fmt.Errorf("Media Foundation minimum display aperture is invalid: bytes=%d offset=%d,%d fraction=%d,%d size=%dx%d", blobSize, area.OffsetX.Value, area.OffsetY.Value, area.OffsetX.Fract, area.OffsetY.Fract, area.Width, area.Height)
		}
		format.cropX, format.cropY = int(area.OffsetX.Value), int(area.OffsetY.Value)
		format.cropWidth, format.cropHeight = int(area.Width), int(area.Height)
	} else if width != decoderWidth || height != decoderHeight {
		return decodedFormat{}, fmt.Errorf("Media Foundation padded storage frame %dx%d lacks a valid minimum display aperture: %w", width, height, apertureErr)
	}
	if format.cropX+format.cropWidth > width || format.cropY+format.cropHeight > height {
		return decodedFormat{}, errors.New("Media Foundation minimum display aperture exceeds the decoded storage frame")
	}
	return format, nil
}

func readRGB32Frame(sample unsafe.Pointer, format decodedFormat) (*image.NRGBA, error) {
	var buffer unsafe.Pointer
	if err := callMethod("IMFSample.ConvertToContiguousBuffer", sample, 41, uintptr(unsafe.Pointer(&buffer))); err != nil {
		return nil, err
	}
	defer release(buffer)
	var data *byte
	var maximum, current uint32
	if err := callMethod("IMFMediaBuffer.Lock", buffer, 3, uintptr(unsafe.Pointer(&data)), uintptr(unsafe.Pointer(&maximum)), uintptr(unsafe.Pointer(&current))); err != nil {
		return nil, err
	}
	rowStride := absolute(format.stride)
	required := rowStride * format.height
	if data == nil || int(current) < required || int(maximum) < required {
		_ = callMethod("IMFMediaBuffer.Unlock", buffer, 4)
		return nil, fmt.Errorf("Media Foundation decoded buffer has %d bytes, expected at least %d", current, required)
	}
	source := unsafe.Slice(data, int(current))
	frame, conversionErr := rgb32ToNRGBA(source, format.width, format.height, format.stride, format.cropX, format.cropY, format.cropWidth, format.cropHeight)
	if err := callMethod("IMFMediaBuffer.Unlock", buffer, 4); err != nil {
		return nil, err
	}
	if conversionErr != nil {
		return nil, conversionErr
	}
	return frame, nil
}
