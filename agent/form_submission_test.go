package agent

import (
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/url"
	"strings"
	"testing"
)

func TestFormSubmitGETUsesSuccessfulControlsInDocumentOrder(t *testing.T) {
	forms := parseForms(t, `<form id="target" action="/submit?fixed=1#done" method="get" enctype="multipart/form-data">
		<input type="hidden" name="_ChArSeT_" value="wrong">
		<input name="dup" value="first">
		<fieldset disabled>
			<legend><input name="legend" value="kept"></legend>
			<input name="disabled_fieldset" value="no">
		</fieldset>
		<input name="dup" value="second">
		<input type="checkbox" name="unchecked" value="no">
		<input type="checkbox" name="checked" checked>
		<input type="radio" name="plan" value="free">
		<input type="radio" name="plan" value="pro" checked>
		<input type="submit" name="submitter" value="go">
		<input type="reset" name="reset" value="no">
		<datalist><input name="datalist" value="no"></datalist>
		<select name="multi" multiple>
			<option value="a" selected>A</option>
			<option value="skip" selected disabled>Skip</option>
			<option value="b" selected>B</option>
		</select>
		<textarea name="note">a
b</textarea>
	</form>
	<input form="target" name="external" value="yes">
	<form id="other"><input name="foreign" value="no"></form>`, "https://e.test/base/page")
	submission, err := forms[0].Submit()
	if err != nil {
		t.Fatal(err)
	}
	if submission.Method != "GET" || submission.ContentType != "" || len(submission.Body) != 0 {
		t.Fatalf("GET submission = %+v", submission)
	}
	parsed, err := url.Parse(submission.URL)
	if err != nil {
		t.Fatal(err)
	}
	wantQuery := "_ChArSeT_=UTF-8&dup=first&legend=kept&dup=second&checked=on&plan=pro&multi=a&multi=b&note=a%0D%0Ab&external=yes"
	if parsed.RawQuery != wantQuery || parsed.Fragment != "done" {
		t.Fatalf("URL = %q, want query %q and fragment done", submission.URL, wantQuery)
	}
}

func TestFormSubmitTextPlainIsByteExact(t *testing.T) {
	forms := parseForms(t, `<form action="submit" method="post" enctype="text/plain">
		<input name="alpha" value="one">
		<input name="alpha" value="two">
		<textarea name="note">a
b
c
d</textarea>
	</form>`, "https://e.test/base/")
	submission, err := forms[0].Submit()
	if err != nil {
		t.Fatal(err)
	}
	if submission.URL != "https://e.test/base/submit" || submission.Method != "POST" || submission.ContentType != FormEncodingText {
		t.Fatalf("submission metadata = %+v", submission)
	}
	want := "alpha=one\r\nalpha=two\r\nnote=a\r\nb\r\nc\r\nd\r\n"
	if string(submission.Body) != want {
		t.Fatalf("text/plain body = %q, want %q", submission.Body, want)
	}
}

func TestSupportedControlValuesMatchBrowserSanitization(t *testing.T) {
	forms := parseForms(t, `<form method="post">
		<input name="unknown" type="unknown" value="line&#10;break">
		<input name="url" type="url" value="  https://e.test/a  ">
		<select name="choice"><option selected>  Alpha
			<script>ignored</script> Beta  </option></select>
	</form>`, "https://e.test/")
	submission, err := forms[0].Submit()
	if err != nil {
		t.Fatal(err)
	}
	if string(submission.Body) != "unknown=linebreak&url=https%3A%2F%2Fe.test%2Fa&choice=Alpha+Beta" {
		t.Fatalf("sanitized body = %q", submission.Body)
	}
}

func TestSelectListBoxWithoutSelectionHasNoSuccessfulValue(t *testing.T) {
	forms := parseForms(t, `<form action="/submit?stale=1#done"><select name="choice" size="2"><option>a</option><option>b</option></select></form>`, "https://e.test/")
	submission, err := forms[0].Submit()
	if err != nil {
		t.Fatal(err)
	}
	if submission.URL != "https://e.test/submit?#done" {
		t.Fatalf("empty GET submission URL = %q", submission.URL)
	}
}

func TestMultipartEncodingIsByteExactAndSubmitPreservesDuplicates(t *testing.T) {
	controls := []formControl{{name: "alpha", value: "one"}, {name: "alpha", value: "two"}, {name: "note\"\r\nline", value: "a\nb"}}
	if _, _, err := encodeMultipartControls(controls, strings.Repeat("x", 71)); err == nil {
		t.Fatal("oversized multipart boundary accepted")
	}
	body, contentType, err := encodeMultipartControls(controls, "Boundary")
	if err != nil {
		t.Fatal(err)
	}
	want := "--Boundary\r\nContent-Disposition: form-data; name=\"alpha\"\r\n\r\none\r\n" +
		"--Boundary\r\nContent-Disposition: form-data; name=\"alpha\"\r\n\r\ntwo\r\n" +
		"--Boundary\r\nContent-Disposition: form-data; name=\"note%22%0D%0Aline\"\r\n\r\na\r\nb\r\n" +
		"--Boundary--\r\n"
	if contentType != "multipart/form-data; boundary=Boundary" || string(body) != want {
		t.Fatalf("multipart content-type=%q body=%q", contentType, body)
	}

	forms := parseForms(t, `<form method="post" enctype="multipart/form-data">
		<input name="alpha" value="one"><input name="alpha" value="two">
	</form>`, "https://e.test/submit")
	submission, err := forms[0].Submit()
	if err != nil {
		t.Fatal(err)
	}
	mediaType, parameters, err := mime.ParseMediaType(submission.ContentType)
	if err != nil || mediaType != FormEncodingMultipart || parameters["boundary"] == "" {
		t.Fatalf("multipart content type = %q, %v", submission.ContentType, err)
	}
	reader := multipart.NewReader(strings.NewReader(string(submission.Body)), parameters["boundary"])
	var values []string
	for {
		part, readErr := reader.NextPart()
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			t.Fatal(readErr)
		}
		value, readErr := io.ReadAll(part)
		if readErr != nil {
			t.Fatal(readErr)
		}
		values = append(values, part.FormName()+"="+string(value))
	}
	if strings.Join(values, ",") != "alpha=one,alpha=two" {
		t.Fatalf("multipart values = %v", values)
	}
}

func TestFormSubmitFileControlRequiresGovernedBrowserUpload(t *testing.T) {
	forms := parseForms(t, `<form method="post" enctype="multipart/form-data">
		<input name="description" value="safe"><input type="file" name="payload" value="/private/secret.txt">
	</form>`, "https://e.test/upload")
	submission, err := forms[0].Submit()
	if !errors.Is(err, ErrFormSubmissionRequiresBrowser) {
		t.Fatalf("file error = %v", err)
	}
	var unsupported *FormSubmissionUnsupportedError
	if !errors.As(err, &unsupported) || unsupported.FieldName != "payload" || unsupported.EncType != FormEncodingMultipart {
		t.Fatalf("typed file error = %+v", unsupported)
	}
	if submission.URL != "" || submission.Method != "" || submission.ContentType != "" || len(submission.Body) != 0 {
		t.Fatalf("file submission leaked partial request = %+v", submission)
	}
	if strings.Contains(err.Error(), "/private/secret.txt") {
		t.Fatalf("file error leaked path: %v", err)
	}
}

func TestFormMetadataNormalizesInvalidMethodAndEncoding(t *testing.T) {
	forms := parseForms(t, `<form method=" post " enctype=" multipart/form-data "></form>`, "https://e.test/")
	if forms[0].Method != "GET" || forms[0].EncType != FormEncodingURLEncoded {
		t.Fatalf("normalized form metadata = %+v", forms[0])
	}
}

func TestURLEncodedSubmissionUsesHTMLFormPercentEncoding(t *testing.T) {
	forms := parseForms(t, `<form method="post"><input name="symbols" value="a ~*+"></form>`, "https://e.test/")
	submission, err := forms[0].Submit()
	if err != nil {
		t.Fatal(err)
	}
	if string(submission.Body) != "symbols=a+%7E*%2B" {
		t.Fatalf("URL-encoded body = %q", submission.Body)
	}
}

func TestDuplicateFormIDDoesNotDuplicateExternalControlOwnership(t *testing.T) {
	forms := parseForms(t, `<form id="duplicate"></form><form id="duplicate"></form><input form="duplicate" name="owner" value="first">`, "https://e.test/")
	first, err := forms[0].Submit()
	if err != nil {
		t.Fatal(err)
	}
	second, err := forms[1].Submit()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first.URL, "owner=first") || strings.Contains(second.URL, "owner=first") {
		t.Fatalf("duplicate ID submissions: first=%q second=%q", first.URL, second.URL)
	}
}

func TestFormSubmitEscalatesDialogAndDirectionality(t *testing.T) {
	tests := []struct {
		name string
		html string
	}{
		{name: "dialog", html: `<form method="dialog"><input name="value" value="safe"></form>`},
		{name: "dirname", html: `<form method="post"><input name="message" dirname="message.dir" value="safe"></form>`},
		{name: "sanitized input", html: `<form method="post"><input type="range" name="amount" value="5"></form>`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			submission, err := parseForms(t, test.html, "https://e.test/")[0].Submit()
			if !errors.Is(err, ErrFormSubmissionRequiresBrowser) || submission.URL != "" || len(submission.Body) != 0 {
				t.Fatalf("submission=%+v err=%v", submission, err)
			}
		})
	}
}
