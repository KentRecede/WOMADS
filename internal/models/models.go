// Package models defines the GORM structs. They mirror db/schema.sql, which is
// the source of truth for the database; we never call AutoMigrate.
//
// Every model sets TableName explicitly so GORM's pluralisation can't drift
// away from the real table names.
package models

import (
	"time"

	"gorm.io/gorm"
)

// ---- roles and status values ---------------------------------------------

const (
	RoleAdministrator  = "administrator"
	RoleHR             = "hr"
	RolePayrollOfficer = "payroll_officer"
	RoleApplicant      = "applicant"
	RoleEmployee       = "employee"
)

var Roles = []string{RoleAdministrator, RoleHR, RolePayrollOfficer, RoleApplicant, RoleEmployee}

func IsValidRole(r string) bool {
	for _, x := range Roles {
		if x == r {
			return true
		}
	}
	return false
}

const (
	JobDraft  = "draft"
	JobActive = "active"
	JobClosed = "closed"

	DeploymentActive = "active"
)

// ---- accounts -------------------------------------------------------------

// User is the application profile for a Supabase Auth account. UserID equals
// auth.users.id. There is no password field on purpose.
type User struct {
	ID        string         `gorm:"column:user_id;type:uuid;primaryKey" json:"id"`
	Role      string         `json:"role"`
	UserName  string         `json:"user_name"`
	Email     string         `json:"email"`
	IsActive  bool           `gorm:"not null;default:true" json:"is_active"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (User) TableName() string { return "users" }

// ---- clients and jobs -----------------------------------------------------

type Client struct {
	ID              string         `gorm:"column:client_id;type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	CompanyName     string         `json:"company_name"`
	ContactPerson   string         `json:"contact_person"`
	NumberOfWorkers int            `json:"number_of_workers"`
	StartDate       Date           `json:"start_date"`
	EndDate         Date           `json:"end_date"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	DeletedAt       gorm.DeletedAt `gorm:"index" json:"-"`
}

func (Client) TableName() string { return "clients" }

type JobOpening struct {
	ID             string         `gorm:"column:job_id;type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	ClientID       string         `gorm:"type:uuid" json:"client_id"`
	PostedBy       *string        `gorm:"type:uuid" json:"posted_by,omitempty"`
	JobTitle       string         `json:"job_title"`
	JobDescription string         `json:"job_description"`
	Location       string         `json:"location"`
	Category       string         `json:"category"`
	Status         string         `json:"status"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (JobOpening) TableName() string { return "job_openings" }

// ---- applicants (module not implemented yet; models ready for it) ---------

type Applicant struct {
	ID             string         `gorm:"column:applicant_id;type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	UserID         string         `gorm:"type:uuid" json:"user_id"`
	JobID          *string        `gorm:"type:uuid" json:"job_id,omitempty"`
	FirstName      string         `json:"first_name"`
	LastName       string         `json:"last_name"`
	MiddleName     *string        `json:"middle_name,omitempty"`
	Nickname       *string        `json:"nickname,omitempty"`
	Address        *string        `json:"address,omitempty"`
	Sex            *string        `json:"sex,omitempty"`
	BirthDate      *Date          `json:"birth_date,omitempty"`
	BirthPlace     *string        `json:"birth_place,omitempty"`
	CivilStatus    *string        `json:"civil_status,omitempty"`
	Religion       *string        `json:"religion,omitempty"`
	Height         *float64       `json:"height,omitempty"`
	Weight         *float64       `json:"weight,omitempty"`
	Citizenship    *string        `json:"citizenship,omitempty"`
	ContactNo      *string        `json:"contact_no,omitempty"`
	Email          *string        `json:"email,omitempty"`
	Status         string         `json:"status"`
	ScreeningScore *float64       `json:"screening_score,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (Applicant) TableName() string { return "applicants" }

type ApplicantEducation struct {
	ID             string  `gorm:"column:education_id;type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	ApplicantID    string  `gorm:"type:uuid" json:"applicant_id"`
	SchoolAttended string  `json:"school_attended"`
	YearStarted    *int    `json:"year_started,omitempty"`
	YearEnded      *int    `json:"year_ended,omitempty"`
	DegreeEarned   *string `json:"degree_earned,omitempty"`
	UnitsEarned    *string `json:"units_earned,omitempty"`
	Qualifications *string `json:"qualifications,omitempty"`
	Certificates   *string `json:"certificates,omitempty"`
}

func (ApplicantEducation) TableName() string { return "applicant_education" }

type ApplicantWorkHistory struct {
	ID            string  `gorm:"column:history_id;type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	ApplicantID   string  `gorm:"type:uuid" json:"applicant_id"`
	Company       string  `json:"company"`
	Position      *string `json:"position,omitempty"`
	ReasonOfLeave *string `json:"reason_of_leave,omitempty"`
}

func (ApplicantWorkHistory) TableName() string { return "applicant_work_history" }

type ApplicantFamily struct {
	ID          string  `gorm:"column:family_id;type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	ApplicantID string  `gorm:"type:uuid" json:"applicant_id"`
	MotherName  *string `json:"mother_name,omitempty"`
	FatherName  *string `json:"father_name,omitempty"`
	Age         *int    `json:"age,omitempty"`
	Status      *string `json:"status,omitempty"`
	Occupation  *string `json:"occupation,omitempty"`
	NoSiblings  *int    `json:"no_siblings,omitempty"`
	Rank        *int    `json:"rank,omitempty"`
}

func (ApplicantFamily) TableName() string { return "applicant_family" }

type ApplicantResidential struct {
	ID             string  `gorm:"column:residential_id;type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	ApplicantID    string  `gorm:"type:uuid" json:"applicant_id"`
	Landmark       *string `json:"landmark,omitempty"`
	Directions     *string `json:"directions,omitempty"`
	LocationSketch *string `json:"location_sketch,omitempty"`
}

func (ApplicantResidential) TableName() string { return "applicant_residential" }

type ApplicantScreening struct {
	ID               string `gorm:"column:screening_id;type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	ApplicantID      string `gorm:"type:uuid" json:"applicant_id"`
	Convicted        *bool  `json:"convicted,omitempty"`
	HasRelatives     *bool  `json:"has_relatives,omitempty"`
	HasDiseases      *bool  `json:"has_diseases,omitempty"`
	HasAppliedBefore *bool  `json:"has_applied_before,omitempty"`
}

func (ApplicantScreening) TableName() string { return "applicant_screening" }

// ---- workforce ------------------------------------------------------------

type Employee struct {
	ID               string         `gorm:"column:employee_id;type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	ApplicantID      *string        `gorm:"type:uuid" json:"applicant_id,omitempty"`
	ClientID         *string        `gorm:"type:uuid" json:"client_id,omitempty"`
	UserID           *string        `gorm:"type:uuid" json:"user_id,omitempty"`
	Name             string         `json:"name"`
	Position         string         `json:"position"`
	EmploymentStatus string         `json:"employment_status"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
	DeletedAt        gorm.DeletedAt `gorm:"index" json:"-"`
}

func (Employee) TableName() string { return "employees" }

type Deployment struct {
	ID         string    `gorm:"column:deployment_id;type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	EmployeeID string    `gorm:"type:uuid" json:"employee_id"`
	ClientID   string    `gorm:"type:uuid" json:"client_id"`
	StartDate  Date      `json:"start_date"`
	EndDate    *Date     `json:"end_date,omitempty"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (Deployment) TableName() string { return "deployments" }

type Attendance struct {
	ID           string  `gorm:"column:attendance_id;type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	DeploymentID string  `gorm:"type:uuid" json:"deployment_id"`
	WorkDate     Date    `json:"work_date"`
	HoursWorked  float64 `json:"hours_worked"`
	OvertimeHrs  float64 `json:"overtime_hrs"`
	NightHrs     float64 `json:"night_hrs"`
	LateMinutes  int     `json:"late_minutes"`
	Verified     bool    `json:"verified"`
	ImportBatch  *string `json:"import_batch,omitempty"`
}

func (Attendance) TableName() string { return "attendance" }

type Payroll struct {
	ID             string    `gorm:"column:payroll_id;type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	EmployeeID     string    `gorm:"type:uuid" json:"employee_id"`
	PeriodStart    Date      `json:"period_start"`
	PeriodEnd      Date      `json:"period_end"`
	HrsWorked      float64   `json:"hrs_worked"`
	OvertimeHrs    float64   `json:"overtime_hrs"`
	NightHrs       float64   `json:"night_hrs"`
	GrossPay       float64   `json:"gross_pay"`
	SSS            float64   `gorm:"column:sss" json:"sss"`
	PhilHealth     float64   `gorm:"column:philhealth" json:"philhealth"`
	PagIBIG        float64   `gorm:"column:pagibig" json:"pagibig"`
	WithholdingTax float64   `json:"withholding_tax"`
	LateDeduction  float64   `json:"late_deduction"`
	NetPay         float64   `json:"net_pay"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
}

func (Payroll) TableName() string { return "payroll" }

// ---- notifications and audit ---------------------------------------------

type Notification struct {
	ID        string    `gorm:"column:notif_id;type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	UserID    string    `gorm:"type:uuid" json:"user_id"`
	Message   string    `json:"message"`
	IsRead    bool      `json:"is_read"`
	CreatedAt time.Time `json:"created_at"`
}

func (Notification) TableName() string { return "notifications" }

type AuditLog struct {
	ID        int64     `gorm:"column:audit_id;primaryKey;autoIncrement" json:"id"`
	ActorID   *string   `gorm:"type:uuid" json:"actor_id,omitempty"`
	Action    string    `json:"action"`
	Entity    string    `json:"entity"`
	EntityID  *string   `json:"entity_id,omitempty"`
	IP        *string   `gorm:"column:ip" json:"ip,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

func (AuditLog) TableName() string { return "audit_logs" }
