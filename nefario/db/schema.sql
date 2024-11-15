
create table Minion (
	uid varchar(32) not NULL,
	serveruid varchar(32) not NULL,
	login varchar(255) not NULL,
	mostrecentprocessrunuid varchar(32),
	description text not NULL,
	extraconfig jsonb default '{}',
	audit_version bigint,
	audit_usergroupuid varchar(32),
	primary key(uid)
)

;;;

create table ProcessConfigFile (
	uid varchar(32) not NULL,
	processdefuid varchar(32) not NULL,
	filename text not NULL,
	"content" text not NULL,
	audit_version bigint,
	extraconfig jsonb default '{}',
	audit_usergroupuid varchar(32),
	primary key(uid)
)

;;;

create table ProcessDef (
	uid varchar(32) not NULL,
	name text not NULL,
	description text not NULL,
	serveruid varchar(32) not NULL,
	harnessname text not NULL,
	harnessdata jsonb default '{}',
	extraconfig jsonb default '{}',
	audit_version bigint,
	audit_usergroupuid varchar(32),
	primary key(uid)
)

;;;

create table ProcessRun (
	uid varchar(32) not NULL,
	processdefuid varchar(32),
	minionuid varchar(32) not NULL,
	parentprocessrunuid varchar(32),
	category text,
	startedat timestamp not NULL,
	completedat timestamp,
	lastpingat timestamp not NULL,
	mailbox varchar(34),
	processstart jsonb default '{}',
	lastping jsonb default '{}',
	processcomplete jsonb default '{}',
	extradata jsonb default '{}',
	serviceuid text not NULL,
	primary key(uid)
)

;;;

create table Server (
	uid varchar(32) not NULL,
	name text not NULL,
	description text not NULL,
	vpndomainname varchar(255) not NULL,
	natsurl varchar(255) not NULL,
	extraconfig jsonb default '{}',
	audit_version bigint,
	audit_usergroupuid varchar(32),
	primary key(uid)
)

;;;

create table Service (
	uid varchar(32) not NULL,
	name varchar(255) not NULL,
	minionuid varchar(32) not NULL,
	minionenabled boolean not NULL,
	extraconfig jsonb,
	systemdunitjson jsonb,
	mostrecentprocessrunuid varchar(32),
	audit_version bigint,
	audit_usergroupuid varchar(32),
	primary key(uid)
)

;;;

CREATE TABLE wal_ping (
    uid VARCHAR(32) PRIMARY KEY,
    data JSONB
)