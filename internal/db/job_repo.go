package db

import "database/sql"

func (d *DB) CreateJob(job Job) (int64, error) {
	res, err := d.Exec(
		`INSERT INTO jobs (name, description, enabled, schedule, backup_type_chain,
		retention_count, retention_days, compression, container_mode, pre_script,
		post_script, notify_on, storage_dest_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.Name, job.Description, job.Enabled, job.Schedule, job.BackupTypeChain,
		job.RetentionCount, job.RetentionDays, job.Compression, job.ContainerMode,
		job.PreScript, job.PostScript, job.NotifyOn, job.StorageDestID,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) GetJob(id int64) (Job, error) {
	var job Job
	err := d.QueryRow(
		`SELECT id, name, description, enabled, schedule, backup_type_chain,
		retention_count, retention_days, compression, container_mode, pre_script,
		post_script, notify_on, storage_dest_id, created_at, updated_at
		FROM jobs WHERE id = ?`, id,
	).Scan(&job.ID, &job.Name, &job.Description, &job.Enabled, &job.Schedule,
		&job.BackupTypeChain, &job.RetentionCount, &job.RetentionDays, &job.Compression,
		&job.ContainerMode, &job.PreScript, &job.PostScript, &job.NotifyOn,
		&job.StorageDestID, &job.CreatedAt, &job.UpdatedAt)
	if err == sql.ErrNoRows {
		return job, ErrNotFound
	}
	return job, err
}

func (d *DB) ListJobs() ([]Job, error) {
	rows, err := d.Query(
		`SELECT id, name, description, enabled, schedule, backup_type_chain,
		retention_count, retention_days, compression, container_mode, pre_script,
		post_script, notify_on, storage_dest_id, created_at, updated_at
		FROM jobs ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []Job
	for rows.Next() {
		var job Job
		if err := rows.Scan(&job.ID, &job.Name, &job.Description, &job.Enabled, &job.Schedule,
			&job.BackupTypeChain, &job.RetentionCount, &job.RetentionDays, &job.Compression,
			&job.ContainerMode, &job.PreScript, &job.PostScript, &job.NotifyOn,
			&job.StorageDestID, &job.CreatedAt, &job.UpdatedAt); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (d *DB) UpdateJob(job Job) error {
	_, err := d.Exec(
		`UPDATE jobs SET name=?, description=?, enabled=?, schedule=?, backup_type_chain=?,
		retention_count=?, retention_days=?, compression=?, container_mode=?, pre_script=?,
		post_script=?, notify_on=?, storage_dest_id=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		job.Name, job.Description, job.Enabled, job.Schedule, job.BackupTypeChain,
		job.RetentionCount, job.RetentionDays, job.Compression, job.ContainerMode,
		job.PreScript, job.PostScript, job.NotifyOn, job.StorageDestID, job.ID,
	)
	return err
}

func (d *DB) DeleteJob(id int64) error {
	_, err := d.Exec("DELETE FROM jobs WHERE id = ?", id)
	return err
}

func (d *DB) AddJobItem(item JobItem) (int64, error) {
	res, err := d.Exec(
		"INSERT INTO job_items (job_id, item_type, item_name, item_id, settings) VALUES (?, ?, ?, ?, ?)",
		item.JobID, item.ItemType, item.ItemName, item.ItemID, item.Settings,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) GetJobItems(jobID int64) ([]JobItem, error) {
	rows, err := d.Query("SELECT id, job_id, item_type, item_name, item_id, settings FROM job_items WHERE job_id = ?", jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []JobItem
	for rows.Next() {
		var item JobItem
		if err := rows.Scan(&item.ID, &item.JobID, &item.ItemType, &item.ItemName, &item.ItemID, &item.Settings); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (d *DB) DeleteJobItems(jobID int64) error {
	_, err := d.Exec("DELETE FROM job_items WHERE job_id = ?", jobID)
	return err
}

// Job Runs

func (d *DB) CreateJobRun(run JobRun) (int64, error) {
	res, err := d.Exec(
		"INSERT INTO job_runs (job_id, status, backup_type, items_total) VALUES (?, ?, ?, ?)",
		run.JobID, run.Status, run.BackupType, run.ItemsTotal,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) UpdateJobRun(run JobRun) error {
	_, err := d.Exec(
		`UPDATE job_runs SET status=?, completed_at=CURRENT_TIMESTAMP, log=?,
		items_done=?, items_failed=?, size_bytes=? WHERE id=?`,
		run.Status, run.Log, run.ItemsDone, run.ItemsFailed, run.SizeBytes, run.ID,
	)
	return err
}

func (d *DB) GetJobRuns(jobID int64, limit int) ([]JobRun, error) {
	rows, err := d.Query(
		`SELECT id, job_id, status, backup_type, started_at, completed_at, log,
		items_total, items_done, items_failed, size_bytes
		FROM job_runs WHERE job_id = ? ORDER BY started_at DESC LIMIT ?`, jobID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var runs []JobRun
	for rows.Next() {
		var run JobRun
		if err := rows.Scan(&run.ID, &run.JobID, &run.Status, &run.BackupType,
			&run.StartedAt, &run.CompletedAt, &run.Log, &run.ItemsTotal,
			&run.ItemsDone, &run.ItemsFailed, &run.SizeBytes); err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

// Restore Points

func (d *DB) CreateRestorePoint(rp RestorePoint) (int64, error) {
	res, err := d.Exec(
		`INSERT INTO restore_points (job_run_id, job_id, backup_type, storage_path, metadata, size_bytes)
		VALUES (?, ?, ?, ?, ?, ?)`,
		rp.JobRunID, rp.JobID, rp.BackupType, rp.StoragePath, rp.Metadata, rp.SizeBytes,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) ListRestorePoints(jobID int64) ([]RestorePoint, error) {
	rows, err := d.Query(
		`SELECT id, job_run_id, job_id, backup_type, storage_path, metadata, size_bytes, created_at
		FROM restore_points WHERE job_id = ? ORDER BY created_at DESC`, jobID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rps []RestorePoint
	for rows.Next() {
		var rp RestorePoint
		if err := rows.Scan(&rp.ID, &rp.JobRunID, &rp.JobID, &rp.BackupType,
			&rp.StoragePath, &rp.Metadata, &rp.SizeBytes, &rp.CreatedAt); err != nil {
			return nil, err
		}
		rps = append(rps, rp)
	}
	return rps, rows.Err()
}

func (d *DB) DeleteRestorePoint(id int64) error {
	_, err := d.Exec("DELETE FROM restore_points WHERE id = ?", id)
	return err
}

func (d *DB) DeleteOldRestorePoints(jobID int64, keepCount int) error {
	_, err := d.Exec(
		`DELETE FROM restore_points WHERE job_id = ? AND id NOT IN
		(SELECT id FROM restore_points WHERE job_id = ? ORDER BY created_at DESC LIMIT ?)`,
		jobID, jobID, keepCount,
	)
	return err
}
