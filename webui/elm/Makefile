 .PHONY: get-deps
get-deps:
	for i in `seq 3`; do \
		npm ci --no-audit && break ; \
	done

build/style.css: styles-in.css tailwind.config.js
	npm run build-css -- -o build/styles.css

.PHONY: build
build: build/style.css
	npm run build-elm -- --output=build/determined-ui.js
	cp -r public/* build/

.PHONY: clean
clean:
	rm -f npm-debug.log
	rm -f build/
	rm -rf elm-stuff/ tests/elm-stuff/ node_modules/

.PHONY: live
live:
	npm run live-workaround -- --output=build/determined-ui.js

.PHONY: fmt
fmt:
	npm run format -- --yes src/ tests/

.PHONY: check
check:
	npm run format -- --validate src/ tests/
	npm run analyse

.PHONY: test
test:
	npm run test
