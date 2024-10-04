# snap-o-matic - Automatic Exoscale Compute Instance Volume Snapshot

```
*** WARNING ***

This is experimental software and may not work as intended or may not be continued in the future.
Use at your own risk.
```

```
*** WARNING ***

Since Exoscale API v2 does not support tags/labels on Snapshots:
Unlike previous versions of snap-o-matic, v0.02 and later will not preserve user-created snapshots.
snap-o-matic will delete the oldest snapshots during rotation and it will not differentiate
between snapshots created by snap-o-matic or by the user.
```

`snap-o-matic` is an automatic snapshot tool for Exoscale Compute instances. It creates snapshots for your instance volumes and cleans up old ones based on customizable retention policies for different timeframes (hourly, daily, weekly, monthly, yearly).

## Installation

You can install `snap-o-matic` by downloading the binaries from the
[releases section](https://github.com/exoscale-labs/snap-o-matic/releases).

## Usage as a Binary

**You should configure snap-o-matic to run from a cron job.** Each run of snap-o-matic creates a snapshot for the specified instance(s) and cleans up old snapshots based on the provided retention policies.

### Command-Line Parameters:

You can run the `snap-o-matic` program with the following parameters:

 - **`-f FILENAME` or `--credentials-file FILENAME`:** File to read API credentials from.
 - **`-d` or `--dry-run`:** Run in dry-run mode (do not actually create or delete snapshots).
 - **`-c CONFIG_FILE` or `--config CONFIG_FILE`:** Path to the YAML configuration file that defines instances and their snapshot retention policies (more on this below).
 - **`-L LOG_LEVEL` or `--log-level LOG_LEVEL`:** Logging level, supported values: `error`, `info`, `debug` (default: `info`).

### Example Cron Job:

To ensure snapshots are created and cleaned up automatically, add snap-o-matic to a cron job that runs at regular intervals. For example, to run every hour:

```bash
0 * * * * /path/to/snap-o-matic -c /path/to/config.yaml >> /var/log/snap-o-matic.log 2>&1
```

## Configuration Using YAML

The YAML configuration file specifies the instances to back up and their snapshot retention policies. Here's an example configuration:

```yaml
instances:
  - id: instance-1-id
    snapshots:
      hourly: 10    # Keep up to 10 hourly snapshots
      daily: 7      # Keep up to 7 daily snapshots
      weekly: 4     # Keep up to 4 weekly snapshots
      monthly: 6    # Keep up to 6 monthly snapshots
      yearly: 2     # Keep up to 2 yearly snapshots

  - id: instance-2-id
    snapshots:
      daily: 14     # Keep up to 14 daily snapshots
      weekly: 3     # Keep up to 3 weekly snapshots
      monthly: 2    # Keep up to 2 monthly snapshots
```

### Retention Policy

`snap-o-matic` supports multiple retention periods for different timeframes:
- **Hourly**: Keeps a set number of snapshots, one for each hour.
- **Daily**: Keeps one snapshot per day for the defined number of days.
- **Weekly**: Keeps one snapshot per week for the defined number of weeks.
- **Monthly**: Keeps one snapshot per month for the defined number of months.
- **Yearly**: Keeps one snapshot per year for the defined number of years.

`snap-o-matic` ensures that only one snapshot is kept for each timeframe (hour, day, week, etc.) and that snapshots from smaller timeframes (e.g., hourly) are not reconsidered for larger timeframes (e.g., daily or weekly).

### Credentials

You can pass your Exoscale API credentials either through a credentials file or environment variables. The supported environment variables are:

- **`EXOSCALE_API_KEY`:** Your Exoscale API key.
- **`EXOSCALE_API_SECRET`:** Your Exoscale API secret.

Alternatively, you can store your credentials in a file and reference it via the `-f` or `--credentials-file` parameter. The credentials file format is as follows:

```text
api_key=EXOabcdef0123456789abcdef01
api_secret=AbCdEfGhIjKlMnOpQrStUvWxYz-0123456789aBcDef
```

### Example Command:

```bash
snap-o-matic -c /path/to/config.yaml -f /path/to/credentials.file -L info
```

This will run the snapshot tool for the instances specified in the configuration file and log output at the `info` level.

## Development and Contribution

This is an experimental tool, and contributions are welcome. Please create a fork and submit a pull request if you would like to contribute to the development of `snap-o-matic`.
