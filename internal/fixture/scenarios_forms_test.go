package fixture

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReadFixtureUploadRejectsBoundExceeded(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "fixture.txt")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, partWriteErr := part.Write([]byte("fixture content")); partWriteErr != nil {
		t.Fatalf("write form file: %v", partWriteErr)
	}
	if writerCloseErr := writer.Close(); writerCloseErr != nil {
		t.Fatalf("close multipart writer: %v", writerCloseErr)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://fixture.test/file-upload", bytes.NewReader(body.Bytes()))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	_, err = readFixtureUpload(httptest.NewRecorder(), req, int64(body.Len()-1))
	if err == nil {
		t.Fatal("expected multipart size limit error")
	}
}
