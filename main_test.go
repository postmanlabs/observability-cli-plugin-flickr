package plugin_flickr

import (
	"testing"

	"github.com/akitasoftware/akita-libs/spec_util"
	"github.com/akitasoftware/akita-libs/test"
)

func TestTransformPath(t *testing.T) {
	p := FlickrAkitaPlugin{}

	testCases := []struct {
		Name         string
		File         string
		ExpectedPath string
	}{
		{
			"multipart body",
			"testdata/2.txt",
			"/services/awesome/my_method",
		},
		{
			"internal api",
			"testdata/3.txt",
			"/services/awesome/my_method",
		},
	}

	for _, tc := range testCases {
		t.Log(tc.Name)

		witness := test.LoadWitnessFromFileOrDie(tc.File)
		m := witness.Method
		err := p.Transform(m)
		if err != nil {
			t.Fatal(err)
		}

		meta := spec_util.HTTPMetaFromMethod(m)
		if meta == nil {
			t.Fatal("missing HTTP metadata in method")
		}

		if meta.PathTemplate != tc.ExpectedPath {
			t.Errorf("Incorrect path tempalate: %q", meta.PathTemplate)
		}
	}
}
