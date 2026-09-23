path: internal/cmd/schema
desc: TypeScript SDK 스키마를 부분 파싱하여 버전별 Go wire 타입을 생성하는 CLI

- 별도 Go 모듈이다. 테스트는 이 디렉터리에서 `go test ./...` 실행.
- 파서는 go-tree-sitter와 TypeScript grammar를 사용한다. 실행에는 CGO와 C 컴파일러가 필요하다.
- 테스트용 실행 파일을 만들지 않는다. `go run` 사용.
- CLI 검증은 프로젝트 루트에서 `go generate ./...` 실행.
- 실제 SDK 스냅샷은 `schema/typescript/{v1,v2}`이며 REVISION에 원본 커밋을 기록한다.
