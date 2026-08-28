package db

import "time"

type Company struct {
	ID        int64
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Unit struct {
	ID               int64
	UnitNumber       string
	SamsaraVehicleID *string
	CompanyID        *int64
	FuelLevelPercent *float64
	MPG              *float64
	MondayItemID     *string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type Driver struct {
	ID              int64
	SamsaraDriverID *string
	DriverName      string
	PhoneNumber     *string
	CompanyID       *int64
	UnitID          *int64
	LoadNumber      *string
	Destination     *string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}
