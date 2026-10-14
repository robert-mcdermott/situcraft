// Test helpers around the decoder's feature counts.

package zstd

// decodeWithStats decodes src with feature counting on.
func decodeWithStats(src []byte, maxSize int) ([]byte, *stats, error) {
	d := new(decoder)
	d.stats = new(stats)
	out, err := d.decode(nil, src, maxSize)
	return out, d.stats, err
}

func (s *stats) add(o *stats) {
	s.frames += o.frames
	s.skippable += o.skippable
	s.checksums += o.checksums
	s.rawBlocks += o.rawBlocks
	s.rleBlocks += o.rleBlocks
	s.compressedBlocks += o.compressedBlocks
	s.rawLiterals += o.rawLiterals
	s.rleLiterals += o.rleLiterals
	s.huffLiterals += o.huffLiterals
	s.treelessLiterals += o.treelessLiterals
	s.oneStream += o.oneStream
	s.fourStreams += o.fourStreams
	s.directWeights += o.directWeights
	s.fseWeights += o.fseWeights
	s.emptySequences += o.emptySequences
	s.sequences += o.sequences
	for k := range s.modes {
		for m := range s.modes[k] {
			s.modes[k][m] += o.modes[k][m]
		}
	}
	for k := range s.repeats {
		s.repeats[k] += o.repeats[k]
	}
	s.overlaps += o.overlaps
	s.crossBlock += o.crossBlock
	s.maxOffset = max(s.maxOffset, o.maxOffset)
	s.windowDescriptor += o.windowDescriptor
	s.singleSegment += o.singleSegment
	s.unknownSize += o.unknownSize
}
