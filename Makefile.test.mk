.PHONY: test trigger sleep snapshots ls

VOLUME  := volkeep_backup
RESTIC  := docker run --rm -e RESTIC_PASSWORD=sample -v $(VOLUME):/repo restic/restic -r /repo

test: trigger sleep snapshots pgverify

trigger:
	docker kill -s SIGUSR1 volkeep

sleep:
	@sleep 2

snapshots:
	$(RESTIC) snapshots

pgverify:
	$(RESTIC) dump latest --tag volkeep_pgdump /data/db.dump \
		| docker exec -i volkeep-postgres pg_restore --list
