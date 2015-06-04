while true ; do 
echo configuring mongodb cluster 
MONGOS=$(etcdctl --peers "$PEERS" ls /services/mongodb/) 

ALL=""
for m in $MONGOS ; do
hostportadmin=$(etcdctl --peers "$PEERS" get $m/host):$(etcdctl --peers $PEERS get $m/port):$(etcdctl --peers $PEERS get $m/admin_port)
ALL="$ALL $hostportadmin"
done

/mongoconf $ALL

echo waiting for mongodb changes
etcdctl --peers "$PEERS" watch --recursive /services/mongodb
done
