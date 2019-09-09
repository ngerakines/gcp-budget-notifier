# gcp-budget-notifier

A daemon that listens to GCP budget notifications on pubsub and sends emails out to specific consumers of those budgets.

```
NAME:
   gcp-budget-notifier - A budget notifier for GCP.

USAGE:
   gcp-budget-notifier [global options] command [command options] [arguments...]

AUTHOR:
   Nick Gerakines <nick@gerakines.net>

COMMANDS:
   help, h  Shows a list of commands or help for one command

GLOBAL OPTIONS:
   --email-from value      The sender of the email [%EMAIL_FROM%]
   --email-host value      The email server to connect to. [%EMAIL_HOST%]
   --email-password value  The password of the email user [%EMAIL_PASSWORD%]
   --email-port value      The email host port. [%EMAIL_PORT%]
   --email-user value      The user to authenticate as [%EMAIL_USER%]
   --project value         The GCP project [%GCP_PROJECT%]
   --recipient value       A recipient of a notification [%RECIPIENTS%]
   --subscription value    The id of the subscription. [%GCP_SUBSCRIPTION%]
   --topic value           The topic to subscribe to. [%GCP_TOPIC%]
   --help, -h              show help
   --version, -v           print the version

COPYRIGHT:
   (c) 2019 Nick Gerakines
```

To set individual budget recipients, the receipient flag can be overloaded using a ";" character.

    $ gcp-budget-notifier --recipient nick@host;id1;id2;id3 --recipient nick@otherhost --recipient nick@yetanother

Or using the environment flag `RECIPIENT`:

    $ export RECIPIENT="nick@host;id1;id2,nick@otherhost;id1,nick@yetanother"
    $ gcp-budget-notifier

Given the above examples:

* nick@host would receive notifications for budgets with the IDs id1 or id2.
* nick@otherhost would receive notifications for the budget id1
* nick@yetanother would receive *all* notifications for all budgets
