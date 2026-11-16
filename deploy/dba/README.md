# 数据库脚本

线上库不给应用账号建表权限。结构变更写在这个目录，由 DBA 在库上执行。应用账号只读写数据。

## 空库第一次

对空库执行一次：

`baseline/multica-schema.sql`

里面是当前全部表、索引和扩展，没有用户、任务等业务数据。末尾的 `schema_migrations` 只记录版本号，用来标记这些变更已经做过。

已经导入过数据的库不要再执行这份基线。

## 以后加字段

不要改基线文件。在 `changes/` 里按顺序新增一个脚本，例如：

`changes/001_issue_add_source.sql`

脚本只写这一次的变更，例如 `ALTER TABLE`。DBA 执行成功后补一行版本记录。`applied_at` 不写就会用当前时间。

```sql
INSERT INTO schema_migrations (version) VALUES ('001_issue_add_source');
```

## 应用启动

接口启动只连接数据库，不建表、不改表。测试和线上都一样。新脚本只交给 DBA，在 `changes/` 里持续追加。
