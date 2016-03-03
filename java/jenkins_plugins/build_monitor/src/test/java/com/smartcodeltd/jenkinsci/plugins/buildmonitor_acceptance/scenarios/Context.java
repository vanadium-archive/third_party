package com.smartcodeltd.jenkinsci.plugins.buildmonitor_acceptance.scenarios;

import com.cloudbees.hudson.plugins.folder.Folder;
import com.smartcodeltd.jenkinsci.plugins.buildmonitor_acceptance.scenarios.adapters.FolderAdapter;
import com.smartcodeltd.jenkinsci.plugins.buildmonitor_acceptance.scenarios.adapters.ProjectCreator;
import hudson.model.FreeStyleProject;
import hudson.tasks.Builder;

import java.io.IOException;
import java.util.List;

public class Context {
    private final ProjectCreator currentRoot;

    public Context(ProjectCreator currentRoot) {
        this.currentRoot = currentRoot;
    }

    public Context createFolder(String name) throws IOException {
        Folder folder = currentRoot.createProject(Folder.class, name);
        return new Context(new FolderAdapter(folder));
    }

    public Context createFreestyleProject(String name, List<Builder> builders, boolean shouldExecute) throws IOException {
        FreeStyleProject project = currentRoot.createProject(FreeStyleProject.class, name);

        project.getBuildersList().addAll(builders);

        if (shouldExecute) {
            project.scheduleBuild2(0);
        }

        return this;
    }
}
