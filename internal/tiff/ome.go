package tiff

// OME-XML, the metadata an OME-TIFF keeps in its first image description: the image's size, pixel
// type and physical pixel size, its channels, and where each plane is (which file, which image
// directory). Read from the OME data model and the OME-TIFF specification (Open Microscopy
// Environment, schema 2016-06), including multi-file OME-TIFFs, as Xenium writes its four
// morphology-focus channels: each file's XML describes all of them and names the file of each.

import (
	"encoding/xml"
	"errors"
	"fmt"
	"strings"
)

// OME is the part of an OME-XML document SituCraft uses.
type OME struct {
	UUID        string
	Images      []OMEImage
	Annotations map[string]map[string]string // MapAnnotation ID → its key/value pairs
}

// OMEImage is one image of an OME document.
type OMEImage struct {
	ID, Name string
	Pixels   OMEPixels
}

// OMEPixels describes an image's planes.
type OMEPixels struct {
	DimensionOrder      string // e.g. "XYZCT"
	Type                string // "uint8", "uint16", "float", …
	SizeX, SizeY, SizeZ int
	SizeC, SizeT        int
	PhysicalSizeX       float64 // 0 when absent
	PhysicalSizeY       float64
	PhysicalSizeZ       float64
	PhysicalSizeXUnit   string // "µm" by default
	PhysicalSizeYUnit   string
	PhysicalSizeZUnit   string
	Channels            []OMEChannel
	TiffData            []OMETiffData
}

// OMEChannel is one channel.
type OMEChannel struct {
	ID, Name        string
	SamplesPerPixel int
	// Annotations are the key/value pairs of the MapAnnotations the channel refers to (Xenium
	// records each channel's "Purpose": Nuclear, Boundary, Interior RNA, Interior protein).
	Annotations map[string]string
}

// OMETiffData maps planes to image directories.
type OMETiffData struct {
	IFD, FirstZ, FirstC, FirstT, PlaneCount int
	// FileName and UUID name the file holding these planes ("" for the file the XML is in).
	FileName, UUID string
	hasPosition    bool
}

// Plane locates one plane: its z, c and t, the file that holds it and the image directory.
type Plane struct {
	Z, C, T  int
	FileName string // "" for the file the XML came from
	IFD      int
}

type xmlOME struct {
	UUID   string     `xml:"UUID,attr"`
	Images []xmlImage `xml:"Image"`
	Maps   []struct {
		ID     string `xml:"ID,attr"`
		Values []struct {
			K     string `xml:"K,attr"`
			Value string `xml:",chardata"`
		} `xml:"Value>M"`
	} `xml:"StructuredAnnotations>MapAnnotation"`
}

type xmlImage struct {
	ID     string `xml:"ID,attr"`
	Name   string `xml:"Name,attr"`
	Pixels struct {
		DimensionOrder    string   `xml:"DimensionOrder,attr"`
		Type              string   `xml:"Type,attr"`
		SizeX             int      `xml:"SizeX,attr"`
		SizeY             int      `xml:"SizeY,attr"`
		SizeZ             int      `xml:"SizeZ,attr"`
		SizeC             int      `xml:"SizeC,attr"`
		SizeT             int      `xml:"SizeT,attr"`
		PhysicalSizeX     *float64 `xml:"PhysicalSizeX,attr"`
		PhysicalSizeY     *float64 `xml:"PhysicalSizeY,attr"`
		PhysicalSizeZ     *float64 `xml:"PhysicalSizeZ,attr"`
		PhysicalSizeXUnit string   `xml:"PhysicalSizeXUnit,attr"`
		PhysicalSizeYUnit string   `xml:"PhysicalSizeYUnit,attr"`
		PhysicalSizeZUnit string   `xml:"PhysicalSizeZUnit,attr"`
		Channels          []struct {
			ID              string `xml:"ID,attr"`
			Name            string `xml:"Name,attr"`
			SamplesPerPixel int    `xml:"SamplesPerPixel,attr"`
			Refs            []struct {
				ID string `xml:"ID,attr"`
			} `xml:"AnnotationRef"`
		} `xml:"Channel"`
		TiffData []struct {
			IFD        *int `xml:"IFD,attr"`
			FirstZ     *int `xml:"FirstZ,attr"`
			FirstC     *int `xml:"FirstC,attr"`
			FirstT     *int `xml:"FirstT,attr"`
			PlaneCount *int `xml:"PlaneCount,attr"`
			UUID       struct {
				FileName string `xml:"FileName,attr"`
				Value    string `xml:",chardata"`
			} `xml:"UUID"`
		} `xml:"TiffData"`
	} `xml:"Pixels"`
}

// ParseOME reads an OME-XML document (an OME-TIFF's first image description).
func ParseOME(description string) (*OME, error) {
	if !strings.Contains(description, "<OME") {
		return nil, errors.New("ome: the description is not OME-XML")
	}
	var doc xmlOME
	if err := xml.Unmarshal([]byte(description), &doc); err != nil {
		return nil, fmt.Errorf("ome: %w", err)
	}
	o := &OME{UUID: doc.UUID, Annotations: map[string]map[string]string{}}
	for _, m := range doc.Maps {
		pairs := map[string]string{}
		for _, v := range m.Values {
			pairs[v.K] = strings.TrimSpace(v.Value)
		}
		o.Annotations[m.ID] = pairs
	}
	for _, img := range doc.Images {
		p := img.Pixels
		px := OMEPixels{
			DimensionOrder: p.DimensionOrder, Type: p.Type,
			SizeX: p.SizeX, SizeY: p.SizeY, SizeZ: max(p.SizeZ, 1), SizeC: max(p.SizeC, 1), SizeT: max(p.SizeT, 1),
			PhysicalSizeXUnit: unitOr(p.PhysicalSizeXUnit), PhysicalSizeYUnit: unitOr(p.PhysicalSizeYUnit), PhysicalSizeZUnit: unitOr(p.PhysicalSizeZUnit),
		}
		if p.PhysicalSizeX != nil {
			px.PhysicalSizeX = *p.PhysicalSizeX
		}
		if p.PhysicalSizeY != nil {
			px.PhysicalSizeY = *p.PhysicalSizeY
		}
		if p.PhysicalSizeZ != nil {
			px.PhysicalSizeZ = *p.PhysicalSizeZ
		}
		if len(px.DimensionOrder) != 5 || !strings.HasPrefix(px.DimensionOrder, "XY") {
			return nil, fmt.Errorf("ome: image %s has dimension order %q", img.ID, px.DimensionOrder)
		}
		for _, c := range p.Channels {
			ch := OMEChannel{ID: c.ID, Name: c.Name, SamplesPerPixel: max(c.SamplesPerPixel, 1), Annotations: map[string]string{}}
			for _, ref := range c.Refs {
				for k, v := range o.Annotations[ref.ID] {
					ch.Annotations[k] = v
				}
			}
			px.Channels = append(px.Channels, ch)
		}
		for _, t := range p.TiffData {
			d := OMETiffData{FileName: t.UUID.FileName, UUID: strings.TrimSpace(t.UUID.Value)}
			if t.IFD != nil {
				d.IFD = *t.IFD
			}
			for _, pair := range []struct {
				src *int
				dst *int
			}{{t.FirstZ, &d.FirstZ}, {t.FirstC, &d.FirstC}, {t.FirstT, &d.FirstT}} {
				if pair.src != nil {
					*pair.dst = *pair.src
					d.hasPosition = true
				}
			}
			switch {
			case t.PlaneCount != nil:
				d.PlaneCount = *t.PlaneCount
			case t.IFD != nil || d.hasPosition:
				// A TiffData that names an IFD or a position describes one plane by default.
				d.PlaneCount = 1
			default:
				// One with no attributes covers every plane, from IFD 0.
				d.PlaneCount = px.SizeZ * px.SizeC * px.SizeT
			}
			px.TiffData = append(px.TiffData, d)
		}
		o.Images = append(o.Images, OMEImage{ID: img.ID, Name: img.Name, Pixels: px})
	}
	if len(o.Images) == 0 {
		return nil, errors.New("ome: the document describes no image")
	}
	return o, nil
}

func unitOr(u string) string {
	if u == "" {
		return "µm"
	}
	return u
}

// Planes lists where every plane of the image is, in the image's dimension order. ownUUID is the
// UUID of the file the XML came from, so that planes it holds get FileName "".
func (px *OMEPixels) Planes(ownUUID string) ([]Plane, error) {
	total := px.SizeZ * px.SizeC * px.SizeT
	if total > maxIFDs {
		return nil, fmt.Errorf("ome: %d planes", total)
	}
	planes := make([]Plane, total)
	assigned := make([]bool, total)
	sizes := map[byte]int{'Z': px.SizeZ, 'C': px.SizeC, 'T': px.SizeT}
	order := px.DimensionOrder[2:]
	// index and position convert between a plane's (z, c, t) and its rank in dimension order.
	index := func(z, c, t int) int {
		pos := map[byte]int{'Z': z, 'C': c, 'T': t}
		i, stride := 0, 1
		for k := 0; k < 3; k++ {
			i += pos[order[k]] * stride
			stride *= sizes[order[k]]
		}
		return i
	}
	position := func(i int) (z, c, t int) {
		pos := map[byte]int{}
		for k := 0; k < 3; k++ {
			pos[order[k]] = i % sizes[order[k]]
			i /= sizes[order[k]]
		}
		return pos['Z'], pos['C'], pos['T']
	}
	if len(px.TiffData) == 0 {
		px.TiffData = []OMETiffData{{PlaneCount: total}}
	}
	for _, d := range px.TiffData {
		if d.FirstZ >= px.SizeZ || d.FirstC >= px.SizeC || d.FirstT >= px.SizeT || d.FirstZ < 0 || d.FirstC < 0 || d.FirstT < 0 || d.IFD < 0 || d.PlaneCount < 0 {
			return nil, errors.New("ome: a TiffData lies outside the image")
		}
		file := d.FileName
		if d.UUID != "" && d.UUID == ownUUID {
			file = ""
		}
		start := index(d.FirstZ, d.FirstC, d.FirstT)
		for k := 0; k < d.PlaneCount; k++ {
			i := start + k
			if i >= total {
				return nil, errors.New("ome: a TiffData runs past the last plane")
			}
			z, c, t := position(i)
			planes[i] = Plane{Z: z, C: c, T: t, FileName: file, IFD: d.IFD + k}
			assigned[i] = true
		}
	}
	for i, ok := range assigned {
		if !ok {
			z, c, t := position(i)
			return nil, fmt.Errorf("ome: plane z=%d c=%d t=%d is in no TiffData", z, c, t)
		}
	}
	return planes, nil
}
