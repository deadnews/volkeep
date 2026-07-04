.PHONY: test trigger wait snapshots pgverify ls

VOLUME  := volkeep_backup
RESTIC  := docker run --rm -e RESTIC_PASSWORD=sample -v $(VOLUME):/repo restic/restic -r /repo
SINCE   := $(shell date +%s)

test: trigger wait snapshots pgverify

trigger:
	docker kill -s SIGUSR1 volkeep

wait:
	@until docker logs --since $(SINCE) volkeep | grep -q "Backup pass finished"; do sleep 1; done

snapshots:
	$(RESTIC) snapshots

pgverify:
	$(RESTIC) dump latest --tag volkeep_pgdump /data/db.dump \
		| docker exec -i volkeep-postgres pg_restore --list
