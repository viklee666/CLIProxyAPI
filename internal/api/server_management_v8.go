package api

// registerManagementV8Routes preserves the custom Pelican UI contract without
// restoring the upstream v8 configuration or credential API.
func (s *Server) registerManagementV8Routes() {
	v8 := s.engine.Group("/v8/management")
	v8.Use(s.managementAvailabilityMiddleware(), s.mgmt.Middleware(), s.mgmt.RequestBodyLimitMiddleware())
	v8.GET("/requests/pelican", s.mgmt.GetPelicanTargetV8)
	v8.POST("/requests/pelican", s.mgmt.PelicanTestV8)
}
