package brew

type templateData struct {
	Name             string
	Desc             string
	Homepage         string
	DownloadURLMac   string
	SHA256Mac        string
	DownloadURLLinux string
	SHA256Linux      string
	Version          string
	Caveats          []string
	Plist            string
	DownloadStrategy string
	Install          []string
	Dependencies     []string
	Conflicts        []string
	Tests            []string
	CustomRequire    string
	CustomBlock      []string
}

const formulaTemplate = `{{ if .CustomRequire -}}
require_relative "{{ .CustomRequire }}"
{{ end -}}
class {{ .Name }} < Formula
  desc "{{ .Desc }}"
  homepage "{{ .Homepage }}"
  version "{{ .Version }}"
  {{- if .DownloadURLMac }}
  if OS.mac?
    url "{{ .DownloadURLMac }}"
    {{- if .DownloadStrategy }}, :using => {{ .DownloadStrategy }}{{- end }}
    sha256 "{{ .SHA256Mac }}"
  end
  {{- end }}
  {{- if .DownloadURLLinux }}
  if OS.linux?
	url "{{ .DownloadURLLinux }}"
    {{- if .DownloadStrategy }}, :using => {{ .DownloadStrategy }}{{- end }}
    sha256 "{{ .SHA256Linux }}"
  end
  {{- end }}

  {{- with .CustomBlock }}
  {{ range $index, $element := . }}
  {{ . }}
  {{- end }}
  {{- end }}

  {{- with .Dependencies }}
  {{ range $index, $element := . }}
  depends_on "{{ . }}"
  {{- end }}
  {{- end -}}

  {{- with .Conflicts }}
  {{ range $index, $element := . }}
  conflicts_with "{{ . }}"
  {{- end }}
  {{- end }}

  def install
    {{- range $index, $element := .Install }}
    {{ . -}}
    {{- end }}
  end

  {{- with .Caveats }}

  def caveats; <<~EOS
    {{- range $index, $element := . }}
    {{ . -}}
    {{- end }}
  EOS
  end
  {{- end -}}

  {{- with .Plist }}

  plist_options :startup => false

  def plist; <<~EOS
    {{ . }}
  EOS
  end
  {{- end -}}

  {{- if .Tests }}

  test do
    {{- range $index, $element := .Tests }}
    {{ . -}}
    {{- end }}
  end
  {{- end }}
end
`
