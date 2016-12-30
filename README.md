# diffrelo

diffrelo is a tool to find the file differences between the remote server and the local workspace.

## Installation

```
go get github.com/kenzo0107/diffrelo
```

## Useage

```
$ diffrelo --help

Usage of diffrelo:
  -ext value
        include file extension. default: php,tpl,js,css,html
  -l string
        local workspace (default "/Users/kenzo/go/src/github.com")
  -r string
        workspace in remote server (default "/var/www/html/")
  -sem int
        semaphore limit count for goroutine (default 5)
  -t string
        target hostname
  -v    show version
  -vext value
        exclude file extension. default: tpl.php,sql,tar.gz
```

## Sample

```
$ diffrelo -t web1 -r /var/www/html -l /Users/kenzo/workspace
```

Target only specific extensions. Use `-ext` option.

```
$ diffrelo -t web1 -r /var/www/html -l /Users/kenzo/workspace -ext php,js
```

Exclude only certain extensions. Use `-vext` option.

```
$ diffrelo -t web1 -r /var/www/html -l /Users/kenzo/workspace -vext tar.gz,zip,tar.bz
```
