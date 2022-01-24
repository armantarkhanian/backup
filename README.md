### Note
Требует доработки и некоторых улучшений
### Принцип работы
- Через MySQL Router REST API програма получает список активных нод кластера.
- Берет любую доступную read-only ноду
- Снимает с нее backup через util.dumpInstace()
- Архивирует полученный дамп в .tar.gz файл
- Загружает его в S3-совместимое объектное хранилище
- Может высылать алерты в Telegram
### Пример конфигурационного файла
```yaml
# config.yml

# секция, указывающая в какие директории класть бекапы и логи
directories:
    backups: /tmp/backups/
    logs: /tmp/logs/ # если не установлена, логи будут писаться в STDOUT

# секция, отвечающая за информацию о кластере 
cluster:
    name: myCluster            # название InnoDB Cluster
    backup-user: root          # mysql пользователь, от имени которого будет выполняться бекап 
    backup-user-password: pass # пароль mysql пользователя

# секция, отвечающая за бекап
backup:
    interval: 1h        # как часто делать бекапы (в Golang Duration формате: 15s, 15m, 24h и т. д.)
    max-backup-files: 3 # сохранять только 3 последних бекапа (остальные будут удалены)

# секция, отвечающая за MySQL Router
mysqlrouter:
    http-addr: 127.0.0.1:8081 # MySQL Router HTTP адрес (для REST запросов)

    basic-auth: # HTTP Basic Auth для MySQL Router REST
        user: user
        password: user

# секция, отвечающая за алерты
alerts:
    level: info # info, error
    telegram:
        turn: on
        bot-token: <TELEGRAM_BOT_TOKEN>
        chat-id: <TELEGRAM_CHAT_ID>
        
# секция, отвечающая за S3-совместимое хранилище, в которое нужно загружать бекапы
s3:
    bucket: innodb-backups
    endpoint: play.min.io
    access-key-id: Q3AM3UQ867SPQQA43P2F
    secret-access-key: zuf+tfteSlswRu7BJ86wekitnifILbZam1KYY3TG
    use-ssl: true
```
