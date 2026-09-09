{{- define "agrelha.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "agrelha.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s" (include "agrelha.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "agrelha.labels" -}}
app.kubernetes.io/name: {{ include "agrelha.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "agrelha.selectorLabels" -}}
app.kubernetes.io/name: {{ include "agrelha.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "agrelha.gitSecretName" -}}
{{- if .Values.git.existingSecret -}}{{ .Values.git.existingSecret }}{{- else -}}{{ include "agrelha.fullname" . }}-git{{- end -}}
{{- end -}}

{{- define "agrelha.oidcSecretName" -}}
{{- if .Values.oidc.existingSecret -}}{{ .Values.oidc.existingSecret }}{{- else -}}{{ include "agrelha.fullname" . }}-oidc{{- end -}}
{{- end -}}
