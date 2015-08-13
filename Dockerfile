FROM fedora
RUN yum -y install golang mongodb

ADD mongoconf.go /
RUN go build mongoconf.go

CMD /mongoconf $ARGS

