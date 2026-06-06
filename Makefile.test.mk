.PHONY: test trigger sleep snapshots ls

VOLUME  := volkeep_backup
RESTIC  := docker run --rm -e RESTIC_PASSWORD=sample -v $(VOLUME):/repo restic/restic -r /repo

test: trigger sleep snapshots

trigger:
	docker kill -s SIGUSR1 volkeep

sleep:
	@sleep 1

snapshots:
	$(RESTIC) snapshots
