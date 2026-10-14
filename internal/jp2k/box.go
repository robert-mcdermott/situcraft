// The JP2 file format (T.800 Annex I): only what is needed to find the codestream, which is all
// SituCraft uses. Colour and resolution boxes are not interpreted.

package jp2k

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

// The JPEG 2000 signature box (I.5.1), always the first 12 bytes of a JP2 file.
var jp2Signature = []byte{0, 0, 0, 12, 'j', 'P', ' ', ' ', 0x0D, 0x0A, 0x87, 0x0A}

const boxJP2C = 0x6A703263 // 'jp2c', the contiguous codestream box (I.5.4)

// codestream returns the codestream in data: data itself when it starts with SOC, else the
// contents of the first contiguous codestream box of a JP2 file.
func codestream(data []byte) ([]byte, error) {
	if len(data) >= 2 && data[0] == 0xFF && data[1] == 0x4F {
		return data, nil
	}
	if len(data) < len(jp2Signature) || !bytes.Equal(data[:len(jp2Signature)], jp2Signature) {
		return nil, errors.New("jp2k: neither a JP2 file nor a JPEG 2000 codestream (no JP2 signature box or SOC marker)")
	}
	be := binary.BigEndian
	for pos := 0; pos < len(data); {
		rest := len(data) - pos
		if rest < 8 {
			return nil, fmt.Errorf("jp2k: JP2 box header at offset %d is truncated", pos)
		}
		lbox, tbox := uint64(be.Uint32(data[pos:])), be.Uint32(data[pos+4:])
		hdr := uint64(8)
		switch lbox {
		case 0: // the box extends to the end of the file
			lbox = uint64(rest)
		case 1: // the length is in the 8-byte XLBox field
			if rest < 16 {
				return nil, fmt.Errorf("jp2k: JP2 box header at offset %d is truncated", pos)
			}
			lbox, hdr = be.Uint64(data[pos+8:]), 16
		}
		if lbox < hdr || lbox > uint64(rest) {
			return nil, fmt.Errorf("jp2k: JP2 box %q at offset %d declares %d bytes, %d remain", boxName(tbox), pos, lbox, rest)
		}
		if tbox == boxJP2C {
			return data[pos+int(hdr) : pos+int(lbox)], nil
		}
		pos += int(lbox)
	}
	return nil, errors.New("jp2k: JP2 file has no contiguous codestream box (jp2c)")
}

func boxName(t uint32) string {
	b := []byte{byte(t >> 24), byte(t >> 16), byte(t >> 8), byte(t)}
	for _, c := range b {
		if c < 0x20 || c > 0x7E {
			return fmt.Sprintf("0x%08X", t)
		}
	}
	return string(b)
}
