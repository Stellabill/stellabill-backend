{{- define "stellabill.labels" -}}
helm.sh/chart: {{ include "stellabill.chart" . }}
{{ include "stellabill.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "stellabill.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "stellabill.selectorLabels" -}}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "stellabill.componentLabels" -}}
{{ include "stellabill.selectorLabels" . }}
app.kubernetes.io/component: {{ .component }}
{{- end }}

{{- define "stellabill.componentSelectorLabels" -}}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: {{ .component }}
{{- end }}

{{- define "stellabill.componentName" -}}
{{ .name }}
{{- end }}
