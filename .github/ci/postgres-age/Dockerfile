# CI twin of the prod postgres image (krolik-server/postgres-age/Dockerfile):
# PostgreSQL 17 + pgvector (from the base image) + Apache AGE built from
# source + hypopg. No initdb/ layer — the workflows create the extensions and
# the app bootstraps its own schema (EnsureSchema / EnsureGraph).
# Keep AGE_VERSION in lockstep with prod: a drift here means CI tests a graph
# engine prod does not run.
FROM pgvector/pgvector:pg17

ARG AGE_VERSION=1.7.0-rc0

RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    postgresql-server-dev-17 \
    bison \
    flex \
    git \
    ca-certificates \
    postgresql-17-hypopg \
    && rm -rf /var/lib/apt/lists/*

# Apache AGE 1.7.0-rc0 — latest release for PostgreSQL 17
RUN git clone --branch "PG17/v${AGE_VERSION}" --depth 1 --single-branch \
    https://github.com/apache/age.git /tmp/age \
    && cd /tmp/age \
    && make PG_CONFIG=/usr/lib/postgresql/17/bin/pg_config \
    && make install PG_CONFIG=/usr/lib/postgresql/17/bin/pg_config \
    && rm -rf /tmp/age
