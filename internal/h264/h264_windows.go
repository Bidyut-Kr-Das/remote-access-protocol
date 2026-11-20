package h264

import (
	"errors"
	"fmt"
	"image"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// com is a raw COM interface pointer; vtbl points at the method table.
type com struct{ vtbl *[64]uintptr }

// call is uintptrescapes so pointers passed as uintptr (out-params, VARIANTs)
// are heap-allocated and kept alive for the duration of the COM call.
//
//go:uintptrescapes
func (c *com) call(idx int, args ...uintptr) uintptr {
	r, _, _ := syscall.SyscallN(c.vtbl[idx], append([]uintptr{uintptr(unsafe.Pointer(c))}, args...)...)
	return r
}

func (c *com) release() {
	if c != nil {
		c.call(2)
	}
}

func failed(hr uintptr) bool { return int32(hr) < 0 }

func hrErr(what string, hr uintptr) error { return fmt.Errorf("h264: %s: hr=%#x", what, uint32(hr)) }

func guid(d1 uint32, d2, d3 uint16, d4 ...byte) windows.GUID {
	g := windows.GUID{Data1: d1, Data2: d2, Data3: d3}
	copy(g.Data4[:], d4)
	return g
}

var (
	clsidEncoder = guid(0x6ca50344, 0x051a, 0x4ded, 0x97, 0x79, 0xa4, 0x33, 0x05, 0x16, 0x5e, 0x35)
	clsidDecoder = guid(0x62ce7e72, 0x4c71, 0x4d20, 0xb1, 0x5d, 0x45, 0x28, 0x31, 0xa8, 0x7d, 0x9d)
	iidTransform = guid(0xbf94c121, 0x5b05, 0x4e6f, 0x80, 0x00, 0xba, 0x59, 0x89, 0x61, 0x41, 0x4d)
	iidCodecAPI  = guid(0x901db4c7, 0x31ce, 0x41a2, 0x85, 0xdc, 0x8f, 0xa0, 0xbf, 0x41, 0xb8, 0xda)

	mtMajorType     = guid(0x48eba18e, 0xf8c9, 0x4687, 0xbf, 0x11, 0x0a, 0x74, 0xc9, 0xf9, 0x6a, 0x8f)
	mtSubtype       = guid(0xf7e34c9a, 0x42e8, 0x4714, 0xb7, 0x4b, 0xcb, 0x29, 0xd7, 0x2c, 0x35, 0xe5)
	mtFrameSize     = guid(0x1652c33d, 0xd6b2, 0x4012, 0xb8, 0x34, 0x72, 0x03, 0x08, 0x49, 0xa3, 0x7d)
	mtFrameRate     = guid(0xc459a2e8, 0x3d2c, 0x4e44, 0xb1, 0x32, 0xfe, 0xe5, 0x15, 0x6c, 0x7b, 0xb0)
	mtInterlace     = guid(0xe2724bb8, 0xe676, 0x4806, 0xb4, 0xb2, 0xa8, 0xd6, 0xef, 0xb4, 0x4c, 0xcd)
	mtAvgBitrate    = guid(0x20332624, 0xfb0d, 0x4d9e, 0xbd, 0x0d, 0xcb, 0xf6, 0x78, 0x6c, 0x10, 0x2e)
	mtProfile       = guid(0xad76a80b, 0x2d5c, 0x4e0b, 0xb3, 0x75, 0x64, 0xe5, 0x20, 0x13, 0x70, 0x36)
	mtDefaultStride = guid(0x644b4e48, 0x1e02, 0x4516, 0xb0, 0xeb, 0xc0, 0x1c, 0xa9, 0xd4, 0x9a, 0xc6)
	mediaTypeVideo  = guid(0x73646976, 0x0000, 0x0010, 0x80, 0x00, 0x00, 0xaa, 0x00, 0x38, 0x9b, 0x71)
	formatH264      = guid(0x34363248, 0x0000, 0x0010, 0x80, 0x00, 0x00, 0xaa, 0x00, 0x38, 0x9b, 0x71)
	formatNV12      = guid(0x3231564e, 0x0000, 0x0010, 0x80, 0x00, 0x00, 0xaa, 0x00, 0x38, 0x9b, 0x71)

	// MF_LOW_LATENCY and CODECAPI_AVLowLatencyMode share this GUID.
	lowLatency     = guid(0x9c27891a, 0xed7a, 0x40e1, 0x88, 0xe8, 0xb2, 0x27, 0x27, 0xa0, 0x24, 0xee)
	apiRateControl = guid(0x1c0608e9, 0x370c, 0x4710, 0x8a, 0x58, 0xcb, 0x61, 0x81, 0xc4, 0x24, 0x23)
	apiMeanBitrate = guid(0xf7222374, 0x2144, 0x4815, 0xb5, 0x50, 0xa3, 0x7f, 0x8e, 0x12, 0xee, 0x52)
	apiGOPSize     = guid(0x95f31b26, 0x95a4, 0x41aa, 0x93, 0x03, 0x24, 0x6a, 0x7f, 0xc6, 0xee, 0xf1)
	apiBFrames     = guid(0x8d390aac, 0xdc5c, 0x4200, 0xb5, 0x7f, 0x81, 0x4d, 0x04, 0xba, 0xba, 0xb2)
	apiForceKeyFrm = guid(0x398c1b98, 0x8353, 0x475a, 0x9e, 0xf2, 0x8f, 0x26, 0x5d, 0x26, 0x03, 0x45)
)

// Vtable indexes (IUnknown occupies 0-2).
const (
	// IMFAttributes / IMFMediaType
	attrGetUINT32 = 7
	attrGetUINT64 = 8
	attrGetGUID   = 10
	attrSetUINT32 = 21
	attrSetUINT64 = 22
	attrSetGUID   = 24
	// IMFTransform
	mftGetOutputStreamInfo    = 7
	mftGetAttributes          = 8
	mftGetOutputAvailableType = 14
	mftSetInputType           = 15
	mftSetOutputType          = 16
	mftGetOutputCurrentType   = 18
	mftProcessMessage         = 23
	mftProcessInput           = 24
	mftProcessOutput          = 25
	// IMFSample
	smpSetSampleTime     = 36
	smpSetSampleDuration = 38
	smpConvertToContig   = 41
	smpAddBuffer         = 42
	// IMFMediaBuffer
	bufLock             = 3
	bufUnlock           = 4
	bufSetCurrentLength = 6
	// ICodecAPI
	apiSetValue = 9
)

const (
	mfeNeedMoreInput = 0xC00D6D72
	mfeStreamChange  = 0xC00D6D61
	mfeNoMoreTypes   = 0xC00D36B9

	msgBeginStreaming = 0x10000000
	msgStartOfStream  = 0x10000003

	providesSamples = 0x100 | 0x200 // MFT_OUTPUT_STREAM_(CAN_)PROVIDES_SAMPLES
	profileBaseline = 66            // eAVEncH264VProfile_Base
)

var (
	mfplat           = windows.NewLazySystemDLL("mfplat.dll")
	mfStartup        = mfplat.NewProc("MFStartup")
	mfCreateType     = mfplat.NewProc("MFCreateMediaType")
	mfCreateSample   = mfplat.NewProc("MFCreateSample")
	mfCreateMemBuf   = mfplat.NewProc("MFCreateMemoryBuffer")
	coCreateInstance = windows.NewLazySystemDLL("ole32.dll").NewProc("CoCreateInstance")
)

var (
	startOnce sync.Once
	startErr  error
)

// startup runs MFStartup once and keeps one thread in the MTA for the life of
// the process. Any thread that never called CoInitialize then belongs to that
// implicit MTA, so goroutines may migrate between OS threads freely; the
// software H.264 MFTs are free-threaded, so no marshaling is involved.
func startup() error {
	startOnce.Do(func() {
		if err := mfplat.Load(); err != nil {
			startErr = fmt.Errorf("h264: %w", err)
			return
		}
		done := make(chan error)
		go func() {
			runtime.LockOSThread()
			// S_FALSE (1): this thread was already in the MTA.
			if err := windows.CoInitializeEx(0, windows.COINIT_MULTITHREADED); err != nil && err != syscall.Errno(1) {
				done <- fmt.Errorf("h264: CoInitializeEx: %w", err)
				return
			}
			if hr, _, _ := mfStartup.Call(0x00020070, 0); failed(hr) { // MF_VERSION, MFSTARTUP_FULL
				done <- hrErr("MFStartup", hr)
				return
			}
			done <- nil
			select {} // hold the MTA open
		}()
		startErr = <-done
	})
	return startErr
}

var (
	availOnce sync.Once
	avail     bool
)

// Available reports whether Media Foundation and both H.264 MFTs work here.
// The result is cached.
func Available() bool {
	availOnce.Do(func() {
		e, err := NewEncoder(64, 64, 30, 500)
		if err != nil {
			return
		}
		e.Close()
		d, err := NewDecoder()
		if err != nil {
			return
		}
		d.Close()
		avail = true
	})
	return avail
}

func createMFT(clsid *windows.GUID) (*com, error) {
	if err := startup(); err != nil {
		return nil, err
	}
	var t *com
	hr, _, _ := coCreateInstance.Call(uintptr(unsafe.Pointer(clsid)), 0, 1, // CLSCTX_INPROC_SERVER
		uintptr(unsafe.Pointer(&iidTransform)), uintptr(unsafe.Pointer(&t)))
	if failed(hr) {
		return nil, hrErr("CoCreateInstance", hr)
	}
	return t, nil
}

func newType(sub *windows.GUID) (*com, error) {
	var t *com
	if hr, _, _ := mfCreateType.Call(uintptr(unsafe.Pointer(&t))); failed(hr) {
		return nil, hrErr("MFCreateMediaType", hr)
	}
	for _, kv := range [][2]*windows.GUID{{&mtMajorType, &mediaTypeVideo}, {&mtSubtype, sub}} {
		if hr := t.call(attrSetGUID, uintptr(unsafe.Pointer(kv[0])), uintptr(unsafe.Pointer(kv[1]))); failed(hr) {
			t.release()
			return nil, hrErr("SetGUID", hr)
		}
	}
	return t, nil
}

func setU32(a *com, key *windows.GUID, v uint32) error {
	if hr := a.call(attrSetUINT32, uintptr(unsafe.Pointer(key)), uintptr(v)); failed(hr) {
		return hrErr("SetUINT32", hr)
	}
	return nil
}

func setU64(a *com, key *windows.GUID, hi, lo uint32) error {
	if hr := a.call(attrSetUINT64, uintptr(unsafe.Pointer(key)), uintptr(uint64(hi)<<32|uint64(lo))); failed(hr) {
		return hrErr("SetUINT64", hr)
	}
	return nil
}

// variant mirrors the 24-byte VARIANT on 64-bit Windows.
type variant struct {
	vt       uint16
	_        [3]uint16
	val, pad uint64
}

func setCodecU32(api *com, key *windows.GUID, v uint32) uintptr {
	return api.call(apiSetValue, uintptr(unsafe.Pointer(key)), uintptr(unsafe.Pointer(&variant{vt: 19, val: uint64(v)}))) // VT_UI4
}

// newSample wraps a fresh memory buffer of size n (len(data) when data != nil).
func newSample(n int, data []byte) (*com, error) {
	var buf, s *com
	if hr, _, _ := mfCreateMemBuf.Call(uintptr(n), uintptr(unsafe.Pointer(&buf))); failed(hr) {
		return nil, hrErr("MFCreateMemoryBuffer", hr)
	}
	defer buf.release()
	if data != nil {
		var p *byte
		if hr := buf.call(bufLock, uintptr(unsafe.Pointer(&p)), 0, 0); failed(hr) {
			return nil, hrErr("Lock", hr)
		}
		copy(unsafe.Slice(p, n), data)
		buf.call(bufUnlock)
		buf.call(bufSetCurrentLength, uintptr(n))
	}
	if hr, _, _ := mfCreateSample.Call(uintptr(unsafe.Pointer(&s))); failed(hr) {
		return nil, hrErr("MFCreateSample", hr)
	}
	if hr := s.call(smpAddBuffer, uintptr(unsafe.Pointer(buf))); failed(hr) {
		s.release()
		return nil, hrErr("AddBuffer", hr)
	}
	return s, nil
}

// sampleBytes copies the sample's payload into dst (appending).
func sampleBytes(s *com, dst []byte) ([]byte, error) {
	var buf *com
	if hr := s.call(smpConvertToContig, uintptr(unsafe.Pointer(&buf))); failed(hr) {
		return dst, hrErr("ConvertToContiguousBuffer", hr)
	}
	defer buf.release()
	var p *byte
	var n uint32
	if hr := buf.call(bufLock, uintptr(unsafe.Pointer(&p)), 0, uintptr(unsafe.Pointer(&n))); failed(hr) {
		return dst, hrErr("Lock", hr)
	}
	dst = append(dst, unsafe.Slice(p, n)...)
	buf.call(bufUnlock)
	return dst, nil
}

type outputBuffer struct {
	streamID uint32
	sample   *com
	status   uint32
	events   *com
}

// pull runs ProcessOutput once. It returns the payload, or nil with the
// failing HRESULT (need-more-input, stream-change, or an error).
func pull(t *com, dst []byte) ([]byte, uintptr, error) {
	var info struct{ flags, size, align uint32 }
	if hr := t.call(mftGetOutputStreamInfo, 0, uintptr(unsafe.Pointer(&info))); failed(hr) {
		return nil, hr, hrErr("GetOutputStreamInfo", hr)
	}
	var ob outputBuffer
	if info.flags&providesSamples == 0 {
		s, err := newSample(int(info.size), nil)
		if err != nil {
			return nil, 0, err
		}
		ob.sample = s
	}
	var status uint32
	hr := t.call(mftProcessOutput, 0, 1, uintptr(unsafe.Pointer(&ob)), uintptr(unsafe.Pointer(&status)))
	defer ob.sample.release()
	defer ob.events.release()
	if failed(hr) {
		return nil, hr, nil
	}
	out, err := sampleBytes(ob.sample, dst)
	return out, 0, err
}

// Encoder turns RGBA frames into H.264 Annex B access units.
// It is not safe for concurrent use.
type Encoder struct {
	mft, api   *com
	w, h       int
	pw, ph     int
	fps, frame int64
	nv12       []byte
}

// NewEncoder creates a low-latency Baseline H.264 encoder for w×h frames.
func NewEncoder(w, h, fps, bitrateKbps int) (*Encoder, error) {
	if w <= 0 || h <= 0 || fps <= 0 || bitrateKbps <= 0 {
		return nil, errors.New("h264: bad encoder parameters")
	}
	t, err := createMFT(&clsidEncoder)
	if err != nil {
		return nil, err
	}
	e := &Encoder{mft: t, w: w, h: h, pw: pad16(w), ph: pad16(h), fps: int64(fps)}
	if err := e.init(bitrateKbps); err != nil {
		e.Close()
		return nil, err
	}
	return e, nil
}

func (e *Encoder) init(kbps int) error {
	t := e.mft
	if hr := t.call(0, uintptr(unsafe.Pointer(&iidCodecAPI)), uintptr(unsafe.Pointer(&e.api))); failed(hr) {
		return hrErr("QueryInterface(ICodecAPI)", hr)
	}
	// Settings that shape the sequence must precede SetOutputType.
	if hr := e.api.call(apiSetValue, uintptr(unsafe.Pointer(&lowLatency)), uintptr(unsafe.Pointer(&variant{vt: 11, val: 0xffff}))); failed(hr) { // VT_BOOL true
		return hrErr("AVLowLatencyMode", hr)
	}
	for _, s := range []struct {
		key *windows.GUID
		v   uint32
	}{
		{&apiRateControl, 0}, // eAVEncCommonRateControlMode_CBR
		{&apiMeanBitrate, uint32(kbps * 1000)},
		{&apiGOPSize, uint32(2 * e.fps)}, // P7: ~2 s GOP
		{&apiBFrames, 0},
	} {
		if hr := setCodecU32(e.api, s.key, s.v); failed(hr) {
			return hrErr("ICodecAPI.SetValue", hr)
		}
	}
	out, err := newType(&formatH264)
	if err != nil {
		return err
	}
	defer out.release()
	if err := errors.Join(
		setU32(out, &mtAvgBitrate, uint32(kbps*1000)),
		setU64(out, &mtFrameRate, uint32(e.fps), 1),
		setU64(out, &mtFrameSize, uint32(e.pw), uint32(e.ph)),
		setU32(out, &mtInterlace, 2), // MFVideoInterlace_Progressive
		setU32(out, &mtProfile, profileBaseline),
	); err != nil {
		return err
	}
	if hr := t.call(mftSetOutputType, 0, uintptr(unsafe.Pointer(out)), 0); failed(hr) {
		return hrErr("encoder SetOutputType", hr)
	}
	in, err := newType(&formatNV12)
	if err != nil {
		return err
	}
	defer in.release()
	if err := errors.Join(
		setU64(in, &mtFrameRate, uint32(e.fps), 1),
		setU64(in, &mtFrameSize, uint32(e.pw), uint32(e.ph)),
		setU32(in, &mtInterlace, 2),
	); err != nil {
		return err
	}
	if hr := t.call(mftSetInputType, 0, uintptr(unsafe.Pointer(in)), 0); failed(hr) {
		return hrErr("encoder SetInputType", hr)
	}
	t.call(mftProcessMessage, msgBeginStreaming, 0)
	t.call(mftProcessMessage, msgStartOfStream, 0)
	e.nv12 = make([]byte, e.pw*e.ph*3/2)
	return nil
}

// Encode compresses one w×h frame and returns its access unit (Annex B).
// The first frame is an IDR with SPS/PPS.
func (e *Encoder) Encode(img *image.RGBA) ([]byte, error) {
	if e.mft == nil {
		return nil, errors.New("h264: encoder closed")
	}
	if b := img.Bounds(); b.Dx() != e.w || b.Dy() != e.h {
		return nil, fmt.Errorf("h264: frame %dx%d, encoder %dx%d", b.Dx(), b.Dy(), e.w, e.h)
	}
	rgbaToNV12(img, e.pw, e.ph, e.nv12)
	s, err := newSample(len(e.nv12), e.nv12)
	if err != nil {
		return nil, err
	}
	const tick = 10_000_000 // 100 ns units
	s.call(smpSetSampleTime, uintptr(e.frame*tick/e.fps))
	s.call(smpSetSampleDuration, uintptr(tick/e.fps))
	e.frame++
	hr := e.mft.call(mftProcessInput, 0, uintptr(unsafe.Pointer(s)), 0)
	s.release()
	if failed(hr) {
		return nil, hrErr("encoder ProcessInput", hr)
	}
	var au []byte
	for {
		out, hr, err := pull(e.mft, au)
		if err != nil {
			return nil, err
		}
		if hr == mfeNeedMoreInput {
			return au, nil
		}
		if failed(hr) {
			return nil, hrErr("encoder ProcessOutput", hr)
		}
		au = out
	}
}

// ForceKeyFrame makes the next Encode emit an IDR frame.
func (e *Encoder) ForceKeyFrame() error {
	if e.api == nil {
		return errors.New("h264: encoder closed")
	}
	if hr := setCodecU32(e.api, &apiForceKeyFrm, 1); failed(hr) {
		return hrErr("AVEncVideoForceKeyFrame", hr)
	}
	return nil
}

// Close releases the encoder. It is safe to call more than once.
func (e *Encoder) Close() {
	if e == nil {
		return
	}
	e.api.release()
	e.mft.release()
	e.api, e.mft = nil, nil
}

// Decoder turns H.264 Annex B access units back into RGBA frames.
// It is not safe for concurrent use.
type Decoder struct {
	mft          *com
	stride, rows int // NV12 layout of the negotiated output
	buf          []byte
}

// NewDecoder creates a low-latency H.264 decoder.
func NewDecoder() (*Decoder, error) {
	t, err := createMFT(&clsidDecoder)
	if err != nil {
		return nil, err
	}
	d := &Decoder{mft: t}
	if err := d.init(); err != nil {
		d.Close()
		return nil, err
	}
	return d, nil
}

func (d *Decoder) init() error {
	var attrs *com
	if hr := d.mft.call(mftGetAttributes, uintptr(unsafe.Pointer(&attrs))); failed(hr) {
		return hrErr("decoder GetAttributes", hr)
	}
	err := setU32(attrs, &lowLatency, 1) // MF_LOW_LATENCY: one output per input
	attrs.release()
	if err != nil {
		return err
	}
	in, err := newType(&formatH264)
	if err != nil {
		return err
	}
	defer in.release()
	if hr := d.mft.call(mftSetInputType, 0, uintptr(unsafe.Pointer(in)), 0); failed(hr) {
		return hrErr("decoder SetInputType", hr)
	}
	if err := d.setOutput(); err != nil {
		return err
	}
	d.mft.call(mftProcessMessage, msgBeginStreaming, 0)
	d.mft.call(mftProcessMessage, msgStartOfStream, 0)
	return nil
}

// setOutput picks the NV12 output type and records its layout. It runs at
// init and again on MF_E_TRANSFORM_STREAM_CHANGE.
func (d *Decoder) setOutput() error {
	for i := 0; ; i++ {
		var t *com
		hr := d.mft.call(mftGetOutputAvailableType, 0, uintptr(i), uintptr(unsafe.Pointer(&t)))
		if hr == mfeNoMoreTypes {
			return errors.New("h264: decoder offers no NV12 output")
		}
		if failed(hr) {
			return hrErr("decoder GetOutputAvailableType", hr)
		}
		var sub windows.GUID
		t.call(attrGetGUID, uintptr(unsafe.Pointer(&mtSubtype)), uintptr(unsafe.Pointer(&sub)))
		if sub != formatNV12 {
			t.release()
			continue
		}
		hr = d.mft.call(mftSetOutputType, 0, uintptr(unsafe.Pointer(t)), 0)
		t.release()
		if failed(hr) {
			return hrErr("decoder SetOutputType", hr)
		}
		break
	}
	var cur *com
	if hr := d.mft.call(mftGetOutputCurrentType, 0, uintptr(unsafe.Pointer(&cur))); failed(hr) {
		return hrErr("decoder GetOutputCurrentType", hr)
	}
	defer cur.release()
	var size uint64
	cur.call(attrGetUINT64, uintptr(unsafe.Pointer(&mtFrameSize)), uintptr(unsafe.Pointer(&size)))
	d.stride, d.rows = int(size>>32), int(uint32(size))
	var stride uint32
	if hr := cur.call(attrGetUINT32, uintptr(unsafe.Pointer(&mtDefaultStride)), uintptr(unsafe.Pointer(&stride))); !failed(hr) && int32(stride) > 0 {
		d.stride = int(stride)
	}
	return nil
}

// Decode feeds one access unit and returns the decoded frame cropped to w×h,
// or (nil, nil) when the decoder has no output yet.
func (d *Decoder) Decode(au []byte, w, h int) (*image.RGBA, error) {
	if d.mft == nil {
		return nil, errors.New("h264: decoder closed")
	}
	if len(au) == 0 {
		return nil, errors.New("h264: empty access unit")
	}
	s, err := newSample(len(au), au)
	if err != nil {
		return nil, err
	}
	hr := d.mft.call(mftProcessInput, 0, uintptr(unsafe.Pointer(s)), 0)
	s.release()
	if failed(hr) {
		return nil, hrErr("decoder ProcessInput", hr)
	}
	var img *image.RGBA
	for {
		out, hr, err := pull(d.mft, d.buf[:0])
		if err != nil {
			return nil, err
		}
		switch {
		case hr == mfeNeedMoreInput:
			return img, nil
		case hr == mfeStreamChange:
			if err := d.setOutput(); err != nil {
				return nil, err
			}
			continue
		case failed(hr):
			return nil, hrErr("decoder ProcessOutput", hr)
		}
		d.buf = out
		if d.stride < w || d.rows < h || len(out) < d.stride*d.rows*3/2 {
			return nil, fmt.Errorf("h264: decoded %dx%d (%d bytes) smaller than %dx%d", d.stride, d.rows, len(out), w, h)
		}
		img = nv12ToRGBA(out, d.stride, d.rows, w, h)
	}
}

// Close releases the decoder. It is safe to call more than once.
func (d *Decoder) Close() {
	if d == nil {
		return
	}
	d.mft.release()
	d.mft = nil
}
