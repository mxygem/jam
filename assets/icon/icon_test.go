package icon

import "testing"

func TestImagesDecodeAtTheirSizes(t *testing.T) {
	imgs, err := Images()
	if err != nil {
		t.Fatal(err)
	}
	if len(imgs) != len(sizes) {
		t.Fatalf("got %d images, want %d", len(imgs), len(sizes))
	}
	for i, img := range imgs {
		if b := img.Bounds(); b.Dx() != sizes[i] || b.Dy() != sizes[i] {
			t.Errorf("image %d is %dx%d, want %d", i, b.Dx(), b.Dy(), sizes[i])
		}
	}
}
