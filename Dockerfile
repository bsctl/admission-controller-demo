########  Start from the latest golang base image
FROM golang:latest as builder
ENV GO111MODULE=on
WORKDIR /app
COPY go.mod .
#COPY go.sum .
#RUN go mod download
COPY cmd .
RUN CGO_ENABLED=0 GOOS=linux go build -o webhook ./...

######## Start a new stage from scratch #######
FROM alpine:3.10
WORKDIR /usr/local/bin
RUN apk --no-cache add ca-certificates
RUN addgroup -S cmp && adduser -u 1200 -S cmp -G cmp
USER 1200
COPY --from=builder /app/webhook .
EXPOSE 8443
WORKDIR /opt/app
CMD ["webhook"]