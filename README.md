ssh postgres@tulip -- psql -c 'delete from processrun'





delete from processrun

delete from service where not minionenabled


nats leaf
	clear nefario-central


nats central
