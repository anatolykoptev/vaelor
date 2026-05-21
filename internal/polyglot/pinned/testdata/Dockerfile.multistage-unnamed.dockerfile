FROM alpine:3.20
RUN echo first

FROM debian:12
RUN echo second

FROM ubuntu:24.04
