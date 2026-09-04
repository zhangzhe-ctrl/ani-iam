env "dp2_04" {
  url = getenv("DATABASE_URL")

  migration {
    dir = "file://migrations"
  }
}
