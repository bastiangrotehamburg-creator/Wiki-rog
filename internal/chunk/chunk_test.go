package chunk

import "testing"

func TestShortTextSingleChunk(t *testing.T) {
	got := Text("kurzer Text", 1000, 150)
	if len(got) != 1 || got[0] != "kurzer Text" {
		t.Fatalf("erwartet 1 Chunk, bekam %v", got)
	}
}

func TestEmptyText(t *testing.T) {
	if got := Text("   ", 1000, 150); got != nil {
		t.Fatalf("erwartet nil, bekam %v", got)
	}
}

func TestLongTextSplits(t *testing.T) {
	var sb string
	for i := 0; i < 20; i++ {
		if i > 0 {
			sb += "\n\n"
		}
		sb += "Absatz " + string(rune('A'+i%26))
		for j := 0; j < 200; j++ {
			sb += "x"
		}
	}
	chunks := Text(sb, 500, 50)
	if len(chunks) < 2 {
		t.Fatalf("erwartet mehrere Chunks, bekam %d", len(chunks))
	}
	for _, c := range chunks {
		if len([]rune(c)) > 650 {
			t.Fatalf("Chunk zu groß: %d Zeichen", len([]rune(c)))
		}
	}
}

func TestOversizedParagraphHardSplit(t *testing.T) {
	var big string
	for i := 0; i < 2500; i++ {
		big += "y"
	}
	chunks := Text(big, 1000, 100)
	if len(chunks) < 3 {
		t.Fatalf("erwartet >=3 Chunks, bekam %d", len(chunks))
	}
	for _, c := range chunks {
		if c == "" {
			t.Fatal("leerer Chunk")
		}
	}
}
