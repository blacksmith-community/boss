BOSS - Blacksmith On-demand ServiceS
====================================

`boss` is a command-line utility for interacting with
[Blacksmith][bs], in case you don't have a Cloud Foundry or
Kubernetes cluster handy.  It's great fun at demos.

![boss is not in any way affiliate with THE BOSS, Bruce Springsteen](boss.jpg)

You can use it to view the Blacksmith Catalog:

```
→ boss catalog
Service     Plans                 Tags
=======     =====                 ====
mariadb     standalone            blacksmith
                                  dedicated
                                  mariadb

postgresql  small-cluster         blacksmith
            standalone            dedicated
                                  postgresql

rabbitmq    cluster               blacksmith
            dedicated             dedicated
                                  rabbitmq

redis       dedicated-cache       blacksmith
            dedicated-persistent  dedicated
                                  redis
```

... or to see what you've provisioned so far:

```
→ boss ls -l
ID                 Service   (ID)      Plan       (ID)
==                 =======   ====      ====       ====
relaxed-tesla      rabbitmq  rabbitmq  dedicated  rabbitmq-dedicated
agitated-jennings  rabbitmq  rabbitmq  dedicated  rabbitmq-dedicated
brave-khorana      rabbitmq  rabbitmq  dedicated  rabbitmq-dedicated
clever-mccarthy    rabbitmq  rabbitmq  dedicated  rabbitmq-dedicated
crazy-murdock      rabbitmq  rabbitmq  dedicated  rabbitmq-dedicated
ecstatic-yonath    rabbitmq  rabbitmq  dedicated  rabbitmq-dedicated
flamboyant-booth   rabbitmq  rabbitmq  dedicated  rabbitmq-dedicated
naughty-solomon    rabbitmq  rabbitmq  dedicated  rabbitmq-dedicated
```

You can create and delete services:

```
→ boss create rabbitmq/dedicated -f
rabbitmq/dedicated instance ecstatic-yonath created.

tailing deployment task log...
Task 10731 | 03:25:01 | Preparing deployment: Preparing deployment started
Task 10731 | 03:25:03 | Preparing deployment: Preparing deployment finished
Task 10731 | 03:25:03 | Preparing package compilation: Finding packages to compile started
Task 10731 | 03:25:03 | Preparing package compilation: Finding packages to compile finished
Task 10731 | 03:25:04 | Creating missing vms: standalone/58bdb1b3-9ff1-49c1-b7c1-badc42a8c892 (0) started
Task 10731 | 03:26:18 | Creating missing vms: standalone/58bdb1b3-9ff1-49c1-b7c1-badc42a8c892 (0) finished
Task 10731 | 03:26:19 | Updating instance: standalone/58bdb1b3-9ff1-49c1-b7c1-badc42a8c892 (0) (canary) started
Task 10731 | 03:26:49 | Updating instance: standalone/58bdb1b3-9ff1-49c1-b7c1-badc42a8c892 (0) (canary) finished

→ boss delete ecstatic-yonath
ecstatic-yonath instance deleted.
```

It can view BOSH manifests, deployment task logs, and service
credentials, too!

```
→ boss task relaxed-tesla
→ boss manifest relaxed-tesla
→ boss creds relaxed-tesla
```

How Do I Contribute?
--------------------

  1. Fork this repo
  2. Create your feature branch (`git checkout -b my-new-feature`)
  3. Commit your changes (`git commit -am 'Added some feature'`)
  4. Push to the branch (`git push origin my-new-feature`)
  5. Create a new Pull Request in Github
  6. Profit!
