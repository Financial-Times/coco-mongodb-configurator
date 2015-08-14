FROM alpine:3.2
ADD mongoconf.go /
RUN echo "@testing http://nl.alpinelinux.org/alpine/edge/testing" >> /etc/apk/repositories \
  && apk --update add go mongodb@testing \
  && go build mongoconf.go \
  && apk del go \
  && rm -rf /var/cache/apk/*

CMD /mongoconf $ARGS
