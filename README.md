Asanawarrior
------------

AW provides bidirectional sync between Asana and Taskwarrior so you can use Taskwarrior as a CLI to Asana. It's being used by various folks at [Dgraph](https://dgraph.io/) for all interactions with [Asana](https://asana.com/) tasks.


## Usage

* Create a Personal Access Token in [Asana](https://asana.com/developers/documentation/getting-started/auth)
  * Visit your account Settings dialog
  * Go to Apps
  * Got to Manage Developer Apps
  * Create a "Personal Access Token" and take note of it
* Get your workspace name from [Asana](https://app.asana.com/) (usually it's the domain name)

``` sh
# Checking available parameters
asanawarrior -h
# Running asanawarrior in verbose mode and avoiding deleting anything
asanawarrior -token <PERSONAL_ACCESS_TOKEN> -domain <WORKSPACE_NAME> -verbose -deletes 0
# Running with default parameters
asanawarrior -token <PERSONAL_ACCESS_TOKEN> -domain <WORKSPACE_NAME>
```
