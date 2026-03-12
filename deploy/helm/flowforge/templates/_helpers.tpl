{{/*
Expand the name of the chart.
*/}}
{{- define "flowforge.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "flowforge.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "flowforge.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "flowforge.labels" -}}
helm.sh/chart: {{ include "flowforge.chart" . }}
{{ include "flowforge.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: flowforge
{{- end }}

{{/*
Selector labels for server
*/}}
{{- define "flowforge.selectorLabels" -}}
app.kubernetes.io/name: {{ include "flowforge.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Server labels
*/}}
{{- define "flowforge.server.labels" -}}
{{ include "flowforge.labels" . }}
app.kubernetes.io/component: server
{{- end }}

{{/*
Server selector labels
*/}}
{{- define "flowforge.server.selectorLabels" -}}
{{ include "flowforge.selectorLabels" . }}
app.kubernetes.io/component: server
{{- end }}

{{/*
Worker labels
*/}}
{{- define "flowforge.worker.labels" -}}
{{ include "flowforge.labels" . }}
app.kubernetes.io/component: worker
{{- end }}

{{/*
Worker selector labels
*/}}
{{- define "flowforge.worker.selectorLabels" -}}
{{ include "flowforge.selectorLabels" . }}
app.kubernetes.io/component: worker
{{- end }}

{{/*
Create the name of the server service account to use
*/}}
{{- define "flowforge.server.serviceAccountName" -}}
{{- if .Values.server.serviceAccount.create }}
{{- default (printf "%s-server" (include "flowforge.fullname" .)) .Values.server.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.server.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Create the name of the worker service account to use
*/}}
{{- define "flowforge.worker.serviceAccountName" -}}
{{- if .Values.worker.serviceAccount.create }}
{{- default (printf "%s-worker" (include "flowforge.fullname" .)) .Values.worker.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.worker.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Create the secret name
*/}}
{{- define "flowforge.secretName" -}}
{{- if .Values.secrets.existingSecret }}
{{- .Values.secrets.existingSecret }}
{{- else }}
{{- include "flowforge.fullname" . }}
{{- end }}
{{- end }}

{{/*
Server image
*/}}
{{- define "flowforge.server.image" -}}
{{- $tag := default .Chart.AppVersion .Values.server.image.tag -}}
{{- printf "%s:%s" .Values.server.image.repository $tag }}
{{- end }}

{{/*
Worker image
*/}}
{{- define "flowforge.worker.image" -}}
{{- $tag := default .Chart.AppVersion .Values.worker.image.tag -}}
{{- printf "%s:%s" .Values.worker.image.repository $tag }}
{{- end }}

{{/*
Migration image
*/}}
{{- define "flowforge.migration.image" -}}
{{- $tag := default .Chart.AppVersion .Values.migrations.image.tag -}}
{{- printf "%s:%s" .Values.migrations.image.repository $tag }}
{{- end }}

{{/*
PostgreSQL host
*/}}
{{- define "flowforge.postgresql.host" -}}
{{- if .Values.postgresql.enabled }}
{{- printf "%s-postgresql" .Release.Name }}
{{- else }}
{{- required "External PostgreSQL host is required when postgresql.enabled=false" .Values.externalPostgresql.host }}
{{- end }}
{{- end }}

{{/*
PostgreSQL port
*/}}
{{- define "flowforge.postgresql.port" -}}
{{- if .Values.postgresql.enabled }}
{{- "5432" }}
{{- else }}
{{- default "5432" .Values.externalPostgresql.port | toString }}
{{- end }}
{{- end }}

{{/*
Database URL
*/}}
{{- define "flowforge.databaseURL" -}}
{{- if .Values.secrets.databaseURL }}
{{- .Values.secrets.databaseURL }}
{{- else }}
{{- printf "postgres://%s:%s@%s:%s/%s?sslmode=require" .Values.secrets.postgres.user .Values.secrets.postgres.password (include "flowforge.postgresql.host" .) (include "flowforge.postgresql.port" .) (default "flowforge" .Values.postgresql.auth.database) }}
{{- end }}
{{- end }}

{{/*
Redis host
*/}}
{{- define "flowforge.redis.host" -}}
{{- if .Values.redis.enabled }}
{{- printf "%s-redis-master" .Release.Name }}
{{- else }}
{{- required "External Redis host is required when redis.enabled=false" .Values.externalRedis.host }}
{{- end }}
{{- end }}

{{/*
Redis URL
*/}}
{{- define "flowforge.redisURL" -}}
{{- if .Values.secrets.redisURL }}
{{- .Values.secrets.redisURL }}
{{- else }}
{{- printf "redis://%s:6379/0" (include "flowforge.redis.host" .) }}
{{- end }}
{{- end }}

{{/*
NATS URL
*/}}
{{- define "flowforge.natsURL" -}}
{{- if .Values.nats.enabled }}
{{- printf "nats://%s-nats:4222" .Release.Name }}
{{- else }}
{{- required "External NATS URL is required when nats.enabled=false" .Values.externalNats.url }}
{{- end }}
{{- end }}

{{/*
Common environment variables shared by server and worker
*/}}
{{- define "flowforge.commonEnv" -}}
- name: DATABASE_URL
  valueFrom:
    secretKeyRef:
      name: {{ include "flowforge.secretName" . }}
      key: database-url
- name: REDIS_URL
  valueFrom:
    secretKeyRef:
      name: {{ include "flowforge.secretName" . }}
      key: redis-url
- name: NATS_URL
  value: {{ include "flowforge.natsURL" . | quote }}
- name: JWT_SECRET
  valueFrom:
    secretKeyRef:
      name: {{ include "flowforge.secretName" . }}
      key: jwt-secret
- name: ENCRYPTION_KEY
  valueFrom:
    secretKeyRef:
      name: {{ include "flowforge.secretName" . }}
      key: encryption-key
- name: LOG_LEVEL
  value: {{ .Values.config.logLevel | quote }}
- name: LOG_FORMAT
  value: {{ .Values.config.logFormat | quote }}
- name: FEATURE_WEBHOOKS
  value: {{ .Values.config.features.webhooks | quote }}
- name: FEATURE_APPROVAL_TASKS
  value: {{ .Values.config.features.approvalTasks | quote }}
- name: FEATURE_PARALLEL_EXECUTION
  value: {{ .Values.config.features.parallelExecution | quote }}
- name: RUN_RETENTION_DAYS
  value: {{ .Values.config.retention.runDays | quote }}
- name: LOG_RETENTION_DAYS
  value: {{ .Values.config.retention.logDays | quote }}
- name: API_RATE_LIMIT
  value: {{ .Values.config.rateLimit.api | quote }}
- name: WEBHOOK_RATE_LIMIT
  value: {{ .Values.config.rateLimit.webhook | quote }}
{{- end }}
